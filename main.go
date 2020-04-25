package main

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/ini.v1"

	mapset "github.com/deckarep/golang-set"
)

type StringSet map[string]struct{}

type Measurement struct {
	Quantity float64 `json:"quantity"`
	Unit     string  `json:"unit"`
}

type BaseUnitConversionMap map[string]*Measurement
type UnitConversionTable map[string]BaseUnitConversionMap
type BaseAliasMap map[string]string
type AliasTable map[string]BaseAliasMap

type UnitConversionContext struct {
	UnitConversionTable
	BaseUnitConversionMap
}

type UnitAliasContext struct {
	AliasTable
	BaseAliasMap
}

type Density struct {
	Quantity   float64
	MassUnit   string
	VolumeUnit string
}
type ProductDensityMap map[string]*Density

type Product struct {
	Name string
	*Measurement
}

type ProductMap map[string]*Product
type RecipeTable map[string]ProductMap
type RecipeSourceMap map[string]string

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
const toTasteUnitName = "to taste"

var unitDescriptionPattern = regexp.MustCompile(`(.+?)\s*\((\d+)\s*(.+)\)`)

func (s StringSet) Sorted() (sorted []string) {
	sorted = make([]string, 0, len(s))
	for element := range s {
		sorted = append(sorted, element)
	}
	sort.Strings(sorted)
	return
}

func NewUnitConversionContext() *UnitConversionContext {
	return &UnitConversionContext{
		UnitConversionTable{},
		BaseUnitConversionMap{},
	}
}

func NewUnitAliasContext() *UnitAliasContext {
	return &UnitAliasContext{
		AliasTable{},
		BaseAliasMap{},
	}
}

func (ctx *UnitConversionContext) ImportFromCSVFile(filename string, productDensityMap ProductDensityMap, productUnitsMap map[string]StringSet) {
	file, err := os.Open(filename)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	csvReader := csv.NewReader(file)
	csvReader.ReuseRecord = true

	unitRecord, err := csvReader.Read()
	if err != nil {
		log.Fatal(err)
	}

	unitDescriptions := unitRecord[1:]
	unitCount := len(unitDescriptions)
	units := make([]string, 0, unitCount)

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
		ctx.BaseUnitConversionMap[unit] = &Measurement{
			Quantity: baseUnitQuantity,
			Unit:     unitDescriptionMatch[3],
		}
	}

	for {
		productRecord, err := csvReader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatal(err)
		}

		product := productRecord[0]

		productUnitSet, ok := productUnitsMap[product]
		if !ok {
			productUnitSet = make(StringSet)
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
			unitDefinition, ok := ctx.UnitConversionTable[unit]
			if !ok {
				unitDefinition = make(BaseUnitConversionMap, unitCount)
				ctx.UnitConversionTable[unit] = unitDefinition
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
				unitBaseDefinition, ok := ctx.BaseUnitConversionMap[unit]
				if ok {
					productDensity.Quantity += productDensityMeasurement.Quantity / unitBaseDefinition.Quantity
					productDensityMeasurementCount++
				}
			}
		}

		productDensity.Quantity /= float64(productDensityMeasurementCount)
		productDensityMap[product] = productDensity
	}
}

func getMeasurement(str string) (m *Measurement) {
	m = &Measurement{}
	_, err := fmt.Sscanln(str, &m.Quantity, &m.Unit)
	if err != nil {
		log.Fatal(err)
	}
	return
}

func (ctx *UnitConversionContext) ImportFromINIFile(filename string, productUnitsMap map[string]StringSet) {
	file, err := ini.Load(filename)
	if err != nil {
		log.Fatal(err)
	}

	baseUnitDefinitions, err := file.GetSection(iniDefaultSectionName)
	if err != nil {
		log.Fatal(err)
	}
	for _, baseUnitDefinition := range baseUnitDefinitions.Keys() {
		ctx.BaseUnitConversionMap[baseUnitDefinition.Name()] = getMeasurement(baseUnitDefinition.Value())
	}

	unitSections := file.Sections()
	for _, unitSection := range unitSections {
		unit := unitSection.Name()
		keys := unitSection.Keys()
		unitDefinition := make(BaseUnitConversionMap, len(keys))

		for _, key := range keys {
			product := key.Name()
			measurement := getMeasurement(key.Value())
			unitDefinition[product] = measurement

			unitSet, ok := productUnitsMap[product]
			if !ok {
				unitSet = make(StringSet, len(unitSections))
				productUnitsMap[product] = unitSet
			}
			unitSet[unit] = struct{}{}
			unitSet[measurement.Unit] = struct{}{}
		}

		ctx.UnitConversionTable[unit] = unitDefinition
	}
}

func (ctx *UnitAliasContext) ImportFromINIFile(filename string) {
	file, err := ini.Load(filename)
	if err != nil {
		log.Fatal(err)
	}

	baseAliasDefinitions, err := file.GetSection(iniDefaultSectionName)
	if err != nil {
		log.Fatal(err)
	}
	for _, baseAliasDefinition := range baseAliasDefinitions.Keys() {
		ctx.BaseAliasMap[baseAliasDefinition.Name()] = baseAliasDefinition.Value()
	}

	for _, unitSection := range file.Sections() {
		aliasDefinitions := unitSection.Keys()
		aliasMap := make(BaseAliasMap, len(aliasDefinitions))

		for _, aliasDefinition := range aliasDefinitions {
			aliasMap[aliasDefinition.Name()] = aliasDefinition.Value()
		}

		ctx.AliasTable[unitSection.Name()] = aliasMap
	}
}

func (m BaseAliasMap) ImportFromINIFile(filename string) {
	file, err := ini.Load(filename)
	if err != nil {
		log.Fatal(err)
	}

	section, err := file.GetSection(iniDefaultSectionName)
	if err != nil {
		log.Fatal(err)
	}
	for _, aliasDefinition := range section.Keys() {
		m[aliasDefinition.Name()] = aliasDefinition.Value()
	}
}

func (m RecipeSourceMap) ImportFromCSVFile(filename string) {
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
	csvReader.ReuseRecord = true

	for {
		recipeRecord, err := csvReader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatal(err)
		}

		recipeName := recipeRecord[0]
		recipeSource := recipeRecord[4]
		m[recipeName] = recipeSource
	}
}

func (p *Product) ConvertUnit(unitConversionContext *UnitConversionContext, unitAliasContext *UnitAliasContext, productAliasMap BaseAliasMap) {
	unitAliasDefinition, ok := unitAliasContext.AliasTable[p.Measurement.Unit]
	if ok {
		unitAlias, ok := unitAliasDefinition[p.Name]
		if !ok {
			unitAlias, ok = unitAliasContext.BaseAliasMap[p.Measurement.Unit]
		}
		if ok {
			p.Measurement.Unit = unitAlias
		}
	}
	productAlias, ok := productAliasMap[p.Name]
	if ok {
		p.Name = productAlias
	}
	var productUnitMeasurement *Measurement
	productUnitDefinition, ok := unitConversionContext.UnitConversionTable[p.Measurement.Unit]
	if ok {
		productUnitMeasurement, ok = productUnitDefinition[p.Name]
	} else {
		productUnitMeasurement, ok = unitConversionContext.BaseUnitConversionMap[p.Measurement.Unit]
	}
	if productUnitMeasurement != nil {
		p.Measurement.Unit = productUnitMeasurement.Unit
		p.Quantity *= productUnitMeasurement.Quantity
	}
}

func (t RecipeTable) ImportFromCSVFile(filename string, products StringSet) {
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
	csvReader.ReuseRecord = true

	for {
		ingredientRecord, err := csvReader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatal(err)
		}

		recipeName := ingredientRecord[3]
		recipe, ok := t[recipeName]
		if !ok {
			recipe = ProductMap{}
			t[recipeName] = recipe
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
			Name: ingredientRecord[0],
			Measurement: &Measurement{
				Quantity: ingredientQuantity,
				Unit:     ingredientRecord[2],
			},
		}
		products[ingredientRecord[0]] = struct{}{}
	}

	return
}

func (t RecipeTable) GetMatchingRecipeNameSets(availableProducts ProductMap, recipeNamePowerSet mapset.Set, productDensityMap ProductDensityMap, numberOfServings int) (matchingRecipeNameSets [][]string) {
	matchingRecipeNameSetsWithSubsets := []mapset.Set{}

	for recipeNameSubsetInterface := range recipeNamePowerSet.Iter() {
		func() {
			remainingProducts := make(ProductMap, len(availableProducts))
			for productName, product := range availableProducts {
				productCopy := *product
				remainingProducts[productName] = &productCopy
			}
			recipeNameSubset := recipeNameSubsetInterface.(mapset.Set)
			for recipeNameInterface := range recipeNameSubset.Iter() {
				recipeName := recipeNameInterface.(string)
				recipe, _ := t[recipeName]
				for _, ingredient := range recipe {
					remainingProduct, ok := remainingProducts[ingredient.Name]
					if !ok {
						return
					}

					if ingredient.Measurement.Unit == toTasteUnitName {
						continue
					}

					convertedIngredientQuantity := ingredient.Quantity * float64(numberOfServings)
					if remainingProduct.Measurement.Unit != ingredient.Measurement.Unit {
						productDensity, ok := productDensityMap[ingredient.Name]
						areUnitsIncomparable := false
						if ok {
							if ingredient.Measurement.Unit == productDensity.VolumeUnit && remainingProduct.Measurement.Unit == productDensity.MassUnit {
								convertedIngredientQuantity *= productDensity.Quantity
							} else if ingredient.Measurement.Unit == productDensity.MassUnit && remainingProduct.Measurement.Unit == productDensity.VolumeUnit {
								convertedIngredientQuantity /= productDensity.Quantity
							} else {
								areUnitsIncomparable = true
							}
						}
						if !ok || areUnitsIncomparable {
							log.Printf(`measurement units "%s" (from product list) and "%s" (from recipe) are incomparable`, remainingProduct.Measurement.Unit, ingredient.Measurement.Unit)
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
				matchingRecipeNameSetsWithSubsets = append(matchingRecipeNameSetsWithSubsets, recipeNameSubset)
			}
		}()
	}

	matchingRecipeNameSets = make([][]string, 0, len(matchingRecipeNameSetsWithSubsets))
	for _, recipeNameLHSSubset := range matchingRecipeNameSetsWithSubsets {
		isSubset := false
		for _, recipeNameRHSSubset := range matchingRecipeNameSetsWithSubsets {
			if recipeNameLHSSubset != recipeNameRHSSubset && recipeNameLHSSubset.IsSubset(recipeNameRHSSubset) {
				isSubset = true
				break
			}
		}
		if !isSubset {
			recipeNameSubsetSlice := make([]string, 0, recipeNameLHSSubset.Cardinality())
			for recipeNameInterface := range recipeNameLHSSubset.Iter() {
				recipeName := recipeNameInterface.(string)
				recipeNameSubsetSlice = append(recipeNameSubsetSlice, recipeName)
			}
			matchingRecipeNameSets = append(matchingRecipeNameSets, recipeNameSubsetSlice)
		}
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

	unitConversionContext := NewUnitConversionContext()
	productDensityMap := ProductDensityMap{}
	productUnitsMap := map[string]StringSet{}
	unitAliasContext := NewUnitAliasContext()
	productAliasMap := BaseAliasMap{}
	if unitConversionTableCSVFilename != "" {
		unitConversionContext.ImportFromCSVFile(unitConversionTableCSVFilename, productDensityMap, productUnitsMap)
	}
	if unitConversionTableINIFilename != "" {
		unitConversionContext.ImportFromINIFile(unitConversionTableINIFilename, productUnitsMap)
	}
	if unitAliasTableFilename != "" {
		unitAliasContext.ImportFromINIFile(unitAliasTableFilename)
	}
	if productAliasMapFilename != "" {
		productAliasMap.ImportFromINIFile(productAliasMapFilename)
	}

	for _, unitDefinition := range unitConversionContext.UnitConversionTable {
		for product := range unitDefinition {
			unitSet, ok := productUnitsMap[product]
			if ok {
				for _, unitBaseDefinition := range unitConversionContext.BaseUnitConversionMap {
					unitSet[unitBaseDefinition.Unit] = struct{}{}
				}
			}
		}
	}

	recipeSources := RecipeSourceMap{}
	recipeSources.ImportFromCSVFile(args[0])

	recipes := make(RecipeTable, len(recipeSources))
	productSet := StringSet{}
	for _, filename := range args[1:] {
		recipes.ImportFromCSVFile(filename, productSet)
	}
	products := productSet.Sorted()

	for _, recipe := range recipes {
		for ingredientName, ingredient := range recipe {
			ingredient.ConvertUnit(unitConversionContext, unitAliasContext, productAliasMap)
			if ingredient.Name != ingredientName {
				recipe[ingredient.Name] = ingredient
				delete(recipe, ingredientName)
			}
		}
	}

	recipeNameSet := mapset.NewThreadUnsafeSet()
	for recipeName := range recipes {
		recipeNameSet.Add(recipeName)
	}

	recipeNamePowerSet := recipeNameSet.PowerSet()

	productsJSON, err := json.Marshal(products)
	if err != nil {
		log.Fatal(err)
	}

	http.HandleFunc("/products", func(w http.ResponseWriter, r *http.Request) {
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

		productUnits := productUnitsMap[request.Product].Sorted()
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
			product.ConvertUnit(unitConversionContext, unitAliasContext, productAliasMap)
			if product.Name != productName {
				request.AvailableProducts[product.Name] = product
				delete(request.AvailableProducts, productName)
			}
		}

		matchingRecipeNameSets := recipes.GetMatchingRecipeNameSets(request.AvailableProducts, recipeNamePowerSet, productDensityMap, request.NumberOfServings)
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
