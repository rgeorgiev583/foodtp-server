package main

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/ini.v1"

	set "github.com/deckarep/golang-set"
)

type StringSet map[string]struct{}

type Measurement struct {
	Quantity float64
	Unit     string
}

type BaseConversionMap map[string]*Measurement
type ConversionTable map[string]BaseConversionMap
type BaseAliasMap map[string]string
type AliasTable map[string]BaseAliasMap

type Density struct {
	Quantity   float64
	MassUnit   string
	VolumeUnit string
}
type DensityMap map[string]*Density

type Product struct {
	Name            string
	Quantity        float64 `json:"quantity"`
	MeasurementUnit string  `json:"measurementUnit"`
}

type ProductMap map[string]*Product
type RecipeTable map[string]ProductMap

type ProductUnitsRequest struct {
	Product string `json:"product"`
}

type RecipeSuggestionsRequest struct {
	NumberOfServings  int        `json:"numberOfServings"`
	AvailableProducts ProductMap `json:"availableProducts"`
}

type RecipeSuggestionsResponse struct {
	Name   string `json:"name"`
	Source string `json:"source"`
}

const iniDefaultSectionName = "DEFAULT"
const fieldNotApplicableStr = "-"

func convertStringSetToSortedSlice(set StringSet) (slice []string) {
	slice = make([]string, 0, len(set))
	for element := range set {
		slice = append(slice, element)
	}
	sort.Strings(slice)
	return
}

func importUnitConversionTableFromCSV(filename string, unitConversionTable ConversionTable, baseUnitConversionMap BaseConversionMap, productDensityMap DensityMap, productUnitsMap map[string]StringSet) {
	file, err := os.Open(filename)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	csvReader := csv.NewReader(file)
	productRecords, err := csvReader.ReadAll()
	if err != nil {
		log.Fatal(err)
	}

	unitDescriptions := productRecords[0][1:]
	unitCount := len(unitDescriptions)
	units := make([]string, 0, unitCount)

	unitDescriptionPattern := regexp.MustCompile(`(.+?)\s*\((\d+)\s*(.+)\)`)
	for _, unitDescription := range unitDescriptions {
		unitDescriptionMatch := unitDescriptionPattern.FindStringSubmatch(unitDescription)
		if len(unitDescriptionMatch) != 4 {
			log.Print("error: invalid format of culinary unit description")
			return
		}
		if err != nil {
			log.Fatal(err)
		}

		unit := unitDescriptionMatch[1]
		units = append(units, unit)

		unitQuantityStr := unitDescriptionMatch[2]
		var baseUnitQuantity float64
		if unitQuantityStr != fieldNotApplicableStr {
			baseUnitQuantity, err = strconv.ParseFloat(unitQuantityStr, 64)
			if err != nil {
				log.Fatal(err)
			}
		}
		baseUnitConversionMap[unit] = &Measurement{
			Quantity: baseUnitQuantity,
			Unit:     unitDescriptionMatch[3],
		}
	}

	for _, productRecord := range productRecords[1:] {
		product := productRecord[0]

		productUnitSet, ok := productUnitsMap[product]
		if !ok {
			productUnitSet = make(StringSet, len(productRecords[1:]))
			productUnitsMap[product] = productUnitSet
		}

		productDensity := &Density{}
		productDensityMeasurementCount := 0

		for i, productDensityMeasurementStr := range productRecord[1:] {
			if productDensityMeasurementStr == fieldNotApplicableStr {
				continue
			}

			productDensityMeasurement := &Measurement{}
			_, err = fmt.Sscanln(productDensityMeasurementStr, &productDensityMeasurement.Quantity, &productDensityMeasurement.Unit)
			if err != nil {
				log.Fatal(err)
			}

			unit := units[i]
			unitDefinition, ok := unitConversionTable[unit]
			if !ok {
				unitDefinition = make(BaseConversionMap, unitCount)
				unitConversionTable[unit] = unitDefinition
			}
			unitDefinition[product] = productDensityMeasurement

			productUnitSet[unit] = struct{}{}
			productUnitSet[productDensityMeasurement.Unit] = struct{}{}

			if productDensity.MassUnit == "" {
				productDensity.MassUnit = productDensityMeasurement.Unit
			}
			if productDensity.VolumeUnit == "" {
				productDensity.VolumeUnit = unit
			}

			if productDensity.MassUnit == productDensityMeasurement.Unit && productDensity.VolumeUnit == unit {
				culinaryUnitBaseDefinition, ok := baseUnitConversionMap[unit]
				if ok {
					productDensity.Quantity += productDensityMeasurement.Quantity / culinaryUnitBaseDefinition.Quantity
					productDensityMeasurementCount++
				}
			}
		}

		productDensity.Quantity /= float64(productDensityMeasurementCount)
		productDensityMap[product] = productDensity
	}
}

func getMeasurement(measurementStr string) (measurement *Measurement) {
	measurement = &Measurement{}
	_, err := fmt.Sscanln(measurementStr, &measurement.Quantity, &measurement.Unit)
	if err != nil {
		log.Fatal(err)
	}
	return
}

func importUnitConversionTableFromINI(filename string, unitConversionTable ConversionTable, baseUnitConversionMap BaseConversionMap, productUnitsMap map[string]StringSet) {
	file, err := ini.Load(filename)
	if err != nil {
		log.Fatal(err)
	}

	baseUnitDefinitions, err := file.GetSection(iniDefaultSectionName)
	if err != nil {
		log.Fatal(err)
	}
	for _, baseUnitDefinition := range baseUnitDefinitions.Keys() {
		baseUnitConversionMap[baseUnitDefinition.Name()] = getMeasurement(baseUnitDefinition.Value())
	}

	sections := file.Sections()
	for _, section := range sections {
		unit := section.Name()
		keys := section.Keys()
		unitDefinition := make(BaseConversionMap, len(keys))

		for _, key := range keys {
			product := key.Name()
			measurement := getMeasurement(key.Value())
			unitDefinition[product] = measurement

			unitSet, ok := productUnitsMap[product]
			if !ok {
				unitSet = make(StringSet, len(sections))
				productUnitsMap[product] = unitSet
			}
			unitSet[unit] = struct{}{}
			unitSet[measurement.Unit] = struct{}{}
		}

		unitConversionTable[unit] = unitDefinition
	}
}

func importUnitAliasTableFromINI(filename string, unitAliasTable AliasTable, baseUnitAliasMap BaseAliasMap) {
	file, err := ini.Load(filename)
	if err != nil {
		log.Fatal(err)
	}

	baseUnitDefinitions, err := file.GetSection(iniDefaultSectionName)
	if err != nil {
		log.Fatal(err)
	}
	for _, baseUnitDefinition := range baseUnitDefinitions.Keys() {
		baseUnitAliasMap[baseUnitDefinition.Name()] = baseUnitDefinition.Value()
	}

	for _, section := range file.Sections() {
		keys := section.Keys()
		unitAliasDefinition := make(BaseAliasMap, len(keys))

		for _, key := range keys {
			unitAliasDefinition[key.Name()] = key.Value()
		}

		unitAliasTable[section.Name()] = unitAliasDefinition
	}
}

func importProductAliasMapFromINI(filename string, productAliasMap BaseAliasMap) {
	file, err := ini.Load(filename)
	if err != nil {
		log.Fatal(err)
	}

	section, err := file.GetSection(iniDefaultSectionName)
	if err != nil {
		log.Fatal(err)
	}
	for _, key := range section.Keys() {
		productAliasMap[key.Name()] = key.Value()
	}
}

func importRecipeMetadataFromCSV(filename string, recipeSources map[string]string) {
	file, err := os.Open(filename)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	bufferedReader := bufio.NewReader(file)
	_, _, err = bufferedReader.ReadLine()
	if err != nil {
		log.Fatal(err)
	}

	csvReader := csv.NewReader(bufferedReader)
	recipeRecords, err := csvReader.ReadAll()
	if err != nil {
		log.Fatal(err)
	}

	for _, recipeRecord := range recipeRecords {
		recipeName := recipeRecord[0]
		recipeSource := recipeRecord[4]
		recipeSources[recipeName] = recipeSource
	}
}

func convertProductUnit(unitConversionTable ConversionTable, baseUnitConversionMap BaseConversionMap, unitAliasTable AliasTable, baseUnitAliasMap BaseAliasMap, productAliasMap BaseAliasMap, product *Product) {
	unitAliasDefinition, ok := unitAliasTable[product.MeasurementUnit]
	if ok {
		unitAlias, ok := unitAliasDefinition[product.Name]
		if !ok {
			unitAlias, ok = baseUnitAliasMap[product.MeasurementUnit]
		}
		if ok {
			product.MeasurementUnit = unitAlias
		}
	}
	productAlias, ok := productAliasMap[product.Name]
	if ok {
		product.Name = productAlias
	}
	var productUnitMeasurement *Measurement
	productUnitDefinition, ok := unitConversionTable[product.MeasurementUnit]
	if ok {
		productUnitMeasurement, ok = productUnitDefinition[product.Name]
	} else {
		productUnitMeasurement, ok = baseUnitConversionMap[product.MeasurementUnit]
	}
	if productUnitMeasurement != nil {
		product.MeasurementUnit = productUnitMeasurement.Unit
		product.Quantity *= productUnitMeasurement.Quantity
	}
}

func importRecipeIngredientsFromCSV(filename string, recipes RecipeTable, products StringSet) {
	file, err := os.Open(filename)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	bufferedReader := bufio.NewReader(file)
	_, _, err = bufferedReader.ReadLine()
	if err != nil {
		log.Fatal(err)
	}

	csvReader := csv.NewReader(bufferedReader)
	ingredientRecords, err := csvReader.ReadAll()
	if err != nil {
		log.Fatal(err)
	}

	for _, ingredientRecord := range ingredientRecords {
		recipeName := ingredientRecord[3]
		recipe, ok := recipes[recipeName]
		if !ok {
			recipe = ProductMap{}
			recipes[recipeName] = recipe
		}
		ingredientQuantityStr := ingredientRecord[1]
		var ingredientQuantity float64
		if ingredientQuantityStr != fieldNotApplicableStr {
			ingredientQuantity, err = strconv.ParseFloat(ingredientQuantityStr, 64)
			if err != nil {
				log.Fatal(err)
			}
		}

		recipe[ingredientRecord[0]] = &Product{
			Name:            ingredientRecord[0],
			Quantity:        ingredientQuantity,
			MeasurementUnit: ingredientRecord[2],
		}
		products[ingredientRecord[0]] = struct{}{}
	}

	return
}

func getMatchingRecipeNameSets(availableProducts ProductMap, recipeNamePowerSet set.Set, recipes RecipeTable, productDensityMap DensityMap, numberOfServings int) (recipeNameMatchingSetSlicesNoSubsets [][]string) {
	recipeNameMatchingSets := []set.Set{}

	for recipeNameSubsetInterface := range recipeNamePowerSet.Iter() {
		func() {
			remainingProducts := make(ProductMap, len(availableProducts))
			for productName, product := range availableProducts {
				productCopy := *product
				remainingProducts[productName] = &productCopy
			}
			recipeNameSubset := recipeNameSubsetInterface.(set.Set)
			for recipeNameInterface := range recipeNameSubset.Iter() {
				recipeName := recipeNameInterface.(string)
				recipe, _ := recipes[recipeName]
				for _, ingredient := range recipe {
					remainingProduct, ok := remainingProducts[ingredient.Name]
					if !ok {
						return
					}

					if ingredient.MeasurementUnit == "на вкус" {
						continue
					}

					convertedIngredientQuantity := ingredient.Quantity * float64(numberOfServings)
					if remainingProduct.MeasurementUnit != ingredient.MeasurementUnit {
						productDensity, ok := productDensityMap[ingredient.Name]
						areUnitsIncomparable := false
						if ok {
							if ingredient.MeasurementUnit == productDensity.VolumeUnit && remainingProduct.MeasurementUnit == productDensity.MassUnit {
								convertedIngredientQuantity *= productDensity.Quantity
							} else if ingredient.MeasurementUnit == productDensity.MassUnit && remainingProduct.MeasurementUnit == productDensity.VolumeUnit {
								convertedIngredientQuantity /= productDensity.Quantity
							} else {
								areUnitsIncomparable = true
							}
						}
						if !ok || areUnitsIncomparable {
							log.Printf(`measurement units "%s" (from product list) and "%s" (from recipe) are incomparable`, remainingProduct.MeasurementUnit, ingredient.MeasurementUnit)
							return
						}
					}

					remainingProduct.Quantity -= convertedIngredientQuantity
					if remainingProduct.Quantity < 0 {
						delete(remainingProducts, remainingProduct.Name)
						return
					}
				}
			}

			if recipeNameSubset.Cardinality() > 0 {
				recipeNameMatchingSets = append(recipeNameMatchingSets, recipeNameSubset)
			}
		}()
	}

	recipeNameMatchingSetsNoSubsets := make([]set.Set, 0, len(recipeNameMatchingSets))
	for _, recipeNameLHSSubset := range recipeNameMatchingSets {
		isSubset := false
		for _, recipeNameRHSSubset := range recipeNameMatchingSets {
			if recipeNameLHSSubset != recipeNameRHSSubset && recipeNameLHSSubset.IsSubset(recipeNameRHSSubset) {
				isSubset = true
				break
			}
		}
		if !isSubset {
			recipeNameMatchingSetsNoSubsets = append(recipeNameMatchingSetsNoSubsets, recipeNameLHSSubset)
		}
	}

	recipeNameMatchingSetSlicesNoSubsets = make([][]string, 0, len(recipeNameMatchingSetsNoSubsets))
	for _, recipeNameSubset := range recipeNameMatchingSetsNoSubsets {
		recipeNameSubsetSlice := make([]string, 0, recipeNameSubset.Cardinality())
		for recipeNameInterface := range recipeNameSubset.Iter() {
			recipeName := recipeNameInterface.(string)
			recipeNameSubsetSlice = append(recipeNameSubsetSlice, recipeName)
		}
		recipeNameMatchingSetSlicesNoSubsets = append(recipeNameMatchingSetSlicesNoSubsets, recipeNameSubsetSlice)
	}

	return
}

func main() {
	var isDebugMode bool
	flag.BoolVar(&isDebugMode, "debug", false, "enable debug mode")

	var port int
	flag.IntVar(&port, "port", 8080, "port to use for the HTTP server")

	var tlsCertFile string
	flag.StringVar(&tlsCertFile, "tlsCertFile", "", "TLS certificate file to use for HTTPS")

	var tlsKeyFile string
	flag.StringVar(&tlsKeyFile, "tlsKeyFile", "", "TLS key file to use for HTTPS")

	var httpOrigin string
	flag.StringVar(&httpOrigin, "httpOrigin", "", "HTTP origin to use with the Access-Control-Allow-Origin response header")
	if isDebugMode {
		httpOrigin = "*"
	}

	var unitConversionTableCSVFilename string
	flag.StringVar(&unitConversionTableCSVFilename, "conversionTableCSV", "", "load a conversion table from a CSV file with the given name")

	var unitConversionTableINIFilename string
	flag.StringVar(&unitConversionTableINIFilename, "conversionTableINI", "", "load a conversion table from an INI file with the given name")

	var unitAliasTableFilename string
	flag.StringVar(&unitAliasTableFilename, "unitAliasTable", "", "load an alias table from an INI file with the given name")

	var productAliasMapFilename string
	flag.StringVar(&productAliasMapFilename, "productAliasMap", "", "load a product alias map from an INI file with the given name")

	flag.Parse()

	args := flag.Args()
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "not enough arguments")
		os.Exit(1)
	}

	unitConversionTable := ConversionTable{}
	baseUnitConversionMap := BaseConversionMap{}
	productDensityMap := DensityMap{}
	productUnitsMap := map[string]StringSet{}
	unitAliasTable := AliasTable{}
	baseUnitAliasMap := BaseAliasMap{}
	productAliasMap := BaseAliasMap{}
	if unitConversionTableCSVFilename != "" {
		importUnitConversionTableFromCSV(unitConversionTableCSVFilename, unitConversionTable, baseUnitConversionMap, productDensityMap, productUnitsMap)
	}
	if unitConversionTableINIFilename != "" {
		importUnitConversionTableFromINI(unitConversionTableINIFilename, unitConversionTable, baseUnitConversionMap, productUnitsMap)
	}
	if unitAliasTableFilename != "" {
		importUnitAliasTableFromINI(unitAliasTableFilename, unitAliasTable, baseUnitAliasMap)
	}
	if productAliasMapFilename != "" {
		importProductAliasMapFromINI(productAliasMapFilename, productAliasMap)
	}

	for _, unitDefinition := range unitConversionTable {
		for product := range unitDefinition {
			unitSet, ok := productUnitsMap[product]
			if ok {
				for _, unitBaseDefinition := range baseUnitConversionMap {
					unitSet[unitBaseDefinition.Unit] = struct{}{}
				}
			}
		}
	}

	recipeSources := map[string]string{}
	importRecipeMetadataFromCSV(args[0], recipeSources)

	recipes := make(RecipeTable, len(recipeSources))
	productSet := StringSet{}
	for _, filename := range args[1:] {
		importRecipeIngredientsFromCSV(filename, recipes, productSet)
	}
	products := convertStringSetToSortedSlice(productSet)

	for _, recipe := range recipes {
		for ingredientName, ingredient := range recipe {
			convertProductUnit(unitConversionTable, baseUnitConversionMap, unitAliasTable, baseUnitAliasMap, productAliasMap, ingredient)
			if ingredient.Name != ingredientName {
				recipe[ingredient.Name] = ingredient
				delete(recipe, ingredientName)
			}
		}
	}

	recipeNameSet := set.NewSet()
	for recipeName := range recipes {
		recipeNameSet.Add(recipeName)
	}

	recipeNamePowerSet := recipeNameSet.PowerSet()

	http.HandleFunc("/products", func(w http.ResponseWriter, r *http.Request) {
		productsJSON, err := json.Marshal(products)
		if err != nil {
			log.Fatal(err)
		}

		w.Header().Add("Content-Type", "application/json")
		if httpOrigin != "" {
			w.Header().Add("Access-Control-Allow-Origin", httpOrigin)
		}
		w.Write(productsJSON)
	})

	http.HandleFunc("/units", func(w http.ResponseWriter, r *http.Request) {
		requestJSON, err := ioutil.ReadAll(r.Body)
		if err != nil {
			log.Fatal(err)
		}
		defer r.Body.Close()

		var request *ProductUnitsRequest
		err = json.Unmarshal(requestJSON, &request)
		if err != nil {
			log.Fatal(err)
		}

		productUnits := convertStringSetToSortedSlice(productUnitsMap[request.Product])
		productUnitsJSON, err := json.Marshal(productUnits)
		if err != nil {
			log.Fatal(err)
		}

		w.Header().Add("Content-Type", "application/json")
		if httpOrigin != "" {
			w.Header().Add("Access-Control-Allow-Origin", httpOrigin)
		}
		w.Write(productUnitsJSON)
	})

	http.HandleFunc("/recipes", func(w http.ResponseWriter, r *http.Request) {
		requestJSON, err := ioutil.ReadAll(r.Body)
		if err != nil {
			log.Fatal(err)
		}
		defer r.Body.Close()

		var request *RecipeSuggestionsRequest
		err = json.Unmarshal(requestJSON, &request)
		if err != nil {
			log.Fatal(err)
		}

		for productName, product := range request.AvailableProducts {
			product.Name = productName
			convertProductUnit(unitConversionTable, baseUnitConversionMap, unitAliasTable, baseUnitAliasMap, productAliasMap, product)
			if product.Name != productName {
				request.AvailableProducts[product.Name] = product
				delete(request.AvailableProducts, productName)
			}
		}

		matchingRecipeNameSets := getMatchingRecipeNameSets(request.AvailableProducts, recipeNamePowerSet, recipes, productDensityMap, request.NumberOfServings)
		for _, matchingRecipeNameSet := range matchingRecipeNameSets {
			fmt.Println(strings.Join(matchingRecipeNameSet, ", "))
		}

		matchingRecipeSetResponseList := make([][]*RecipeSuggestionsResponse, 0, len(matchingRecipeNameSets))
		for _, matchingRecipeNameSet := range matchingRecipeNameSets {
			matchingRecipeSetResponse := make([]*RecipeSuggestionsResponse, 0, len(matchingRecipeNameSet))
			for _, matchingRecipeName := range matchingRecipeNameSet {
				matchingRecipeSource, ok := recipeSources[matchingRecipeName]
				if !ok {
					log.Fatal("recipe not found")
				}

				matchingRecipeResponse := &RecipeSuggestionsResponse{
					Name:   matchingRecipeName,
					Source: matchingRecipeSource,
				}
				matchingRecipeSetResponse = append(matchingRecipeSetResponse, matchingRecipeResponse)
			}
			matchingRecipeSetResponseList = append(matchingRecipeSetResponseList, matchingRecipeSetResponse)
		}

		matchingRecipeSetResponseListJSON, err := json.Marshal(matchingRecipeSetResponseList)
		if err != nil {
			log.Fatal(err)
		}

		w.Header().Add("Content-Type", "application/json")
		if httpOrigin != "" {
			w.Header().Add("Access-Control-Allow-Origin", httpOrigin)
		}
		w.Write(matchingRecipeSetResponseListJSON)
	})

	if tlsCertFile != "" && tlsKeyFile != "" {
		log.Fatal(http.ListenAndServeTLS(fmt.Sprintf(":%v", port), tlsCertFile, tlsKeyFile, nil))
	} else {
		log.Fatal(http.ListenAndServe(fmt.Sprintf(":%v", port), nil))
	}
}
