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

	set "github.com/deckarep/golang-set"
)

type Measurement struct {
	Quantity float64
	Unit     string
}

type BaseConversionTable map[string]*Measurement
type ConversionTable map[string]BaseConversionTable
type AliasTable map[string]string
type UnitAliasTable map[string]AliasTable

type Product struct {
	Name            string
	Quantity        float64 `json:"quantity"`
	MeasurementUnit string  `json:"unit"`
}

type ProductMap map[string]*Product
type RecipeMap map[string]ProductMap
type RecipeSourceMap map[string]string

type RecipeSuggestionRequest struct {
	NumberOfServings  int        `json:"numberOfServings"`
	AvailableProducts ProductMap `json:"products"`
}

type RecipeSuggestionResponse struct {
	Name   string `json:"name"`
	Source string `json:"source"`
}

func loadConversionTableCSV(filename string, conversionTable ConversionTable, baseConversionTable BaseConversionTable) {
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

	culinaryUnitDescriptions := productRecords[0][1:]
	culinaryUnitCount := len(culinaryUnitDescriptions)
	culinaryUnits := make([]string, culinaryUnitCount, culinaryUnitCount)

	culinaryUnitDescriptionPattern := regexp.MustCompile(`(.+?)\s*\((\d+)\s*(.+)\)`)
	for i, culinaryUnitDescription := range culinaryUnitDescriptions {
		culinaryUnitDescriptionMatch := culinaryUnitDescriptionPattern.FindStringSubmatch(culinaryUnitDescription)
		if len(culinaryUnitDescriptionMatch) != 4 {
			log.Print("error: invalid format of culinary unit description")
			return
		}
		if err != nil {
			log.Fatal(err)
		}

		culinaryUnit := culinaryUnitDescriptionMatch[1]
		culinaryUnits[i] = culinaryUnit

		measurementQuantityStr := culinaryUnitDescriptionMatch[2]
		var measurementQuantity float64
		if measurementQuantityStr != "-" {
			measurementQuantity, err = strconv.ParseFloat(measurementQuantityStr, 64)
			if err != nil {
				log.Fatal(err)
			}
		}
		baseConversionTable[culinaryUnit] = &Measurement{
			Quantity: measurementQuantity,
			Unit:     culinaryUnitDescriptionMatch[3],
		}
	}

	for _, productRecord := range productRecords[1:] {
		for i, measurementStr := range productRecord[1:] {
			if measurementStr == "-" {
				continue
			}

			measurement := &Measurement{}
			_, err = fmt.Sscanln(measurementStr, &measurement.Quantity, &measurement.Unit)
			if err != nil {
				log.Fatal(err)
			}

			unitDefinition, ok := conversionTable[culinaryUnits[i]]
			if !ok {
				unitDefinition = BaseConversionTable{}
				conversionTable[culinaryUnits[i]] = unitDefinition
			}

			unitDefinition[productRecord[0]] = measurement
		}
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

func loadConversionTableINI(filename string, conversionTable ConversionTable, baseConversionTable BaseConversionTable) {
	file, err := ini.Load(filename)
	if err != nil {
		log.Fatal(err)
	}

	baseUnitDefinitions, err := file.GetSection("DEFAULT")
	if err != nil {
		log.Fatal(err)
	}
	for _, key := range baseUnitDefinitions.Keys() {
		baseConversionTable[key.Name()] = getMeasurement(key.Value())
	}

	for _, section := range file.Sections() {
		unitDefinition := BaseConversionTable{}

		for _, key := range section.Keys() {
			unitDefinition[key.Name()] = getMeasurement(key.Value())
		}

		conversionTable[section.Name()] = unitDefinition
	}
}

func loadUnitAliasTable(filename string, unitAliasTable UnitAliasTable, baseUnitAliasTable AliasTable) {
	file, err := ini.Load(filename)
	if err != nil {
		log.Fatal(err)
	}

	baseUnitDefinitions, err := file.GetSection("DEFAULT")
	if err != nil {
		log.Fatal(err)
	}
	for _, key := range baseUnitDefinitions.Keys() {
		baseUnitAliasTable[key.Name()] = key.Value()
	}

	for _, section := range file.Sections() {
		unitAliasDefinition := AliasTable{}

		for _, key := range section.Keys() {
			unitAliasDefinition[key.Name()] = key.Value()
		}

		unitAliasTable[section.Name()] = unitAliasDefinition
	}
}

func loadProductAliasTable(filename string, productAliasTable AliasTable) {
	file, err := ini.Load(filename)
	if err != nil {
		log.Fatal(err)
	}

	section, err := file.GetSection("DEFAULT")
	if err != nil {
		log.Fatal(err)
	}
	for _, key := range section.Keys() {
		productAliasTable[key.Name()] = key.Value()
	}
}

func loadRecipeMetadata(reader io.Reader, recipeSources RecipeSourceMap) {
	bufferedReader := bufio.NewReader(reader)
	_, _, err := bufferedReader.ReadLine()
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

func getProductUnitMeasurement(unitConversionTable ConversionTable, baseConversionTable BaseConversionTable, product *Product) (productUnitMeasurement *Measurement) {
	productUnitDefinition, ok := unitConversionTable[product.MeasurementUnit]
	if ok {
		productUnitMeasurement, ok = productUnitDefinition[product.Name]
	} else {
		productUnitMeasurement, ok = baseConversionTable[product.MeasurementUnit]
	}
	return
}

func convertProductUnit(unitConversionTable ConversionTable, baseConversionTable BaseConversionTable, unitAliasTable UnitAliasTable, baseUnitAliasTable AliasTable, productAliasTable AliasTable, product *Product) {
	unitAliasDefinition, ok := unitAliasTable[product.MeasurementUnit]
	if ok {
		unitAlias, ok := unitAliasDefinition[product.Name]
		if !ok {
			unitAlias, ok = baseUnitAliasTable[product.MeasurementUnit]
		}
		if ok {
			product.MeasurementUnit = unitAlias
		}
	}
	productAlias, ok := productAliasTable[product.Name]
	if ok {
		product.Name = productAlias
	}
	productUnitMeasurement := getProductUnitMeasurement(unitConversionTable, baseConversionTable, product)
	if productUnitMeasurement != nil {
		product.MeasurementUnit = productUnitMeasurement.Unit
		product.Quantity *= productUnitMeasurement.Quantity
	}
}

func importRecipesFromCSV(reader io.Reader, recipes RecipeMap) {
	bufferedReader := bufio.NewReader(reader)
	_, _, err := bufferedReader.ReadLine()
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
		if ingredientQuantityStr != "-" {
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
	}

	return
}

func uniq(slice []string) (uniqSlice []string) {
	uniqSlice = make([]string, 0, cap(slice))
	if len(slice) == 0 {
		return
	}

	uniqSlice = append(uniqSlice, slice[0])
	previousElement := slice[0]
	for _, element := range slice {
		if previousElement == element {
			continue
		}

		uniqSlice = append(uniqSlice, element)
		previousElement = element
	}
	return
}

func getSupportedProducts(recipes RecipeMap) (products []string) {
	products = []string{}
	for _, recipe := range recipes {
		for _, ingredient := range recipe {
			products = append(products, ingredient.Name)
		}
	}
	sort.Strings(products)
	products = uniq(products)
	return
}

func scaleRecipesByNumberOfServings(recipes RecipeMap, numberOfServings int) {
	for _, recipe := range recipes {
		for _, ingredient := range recipe {
			ingredient.Quantity *= 2
		}
	}
}

func getPossibleRecipeSets(availableProducts ProductMap, recipes RecipeMap) (recipeNameMatchingSetSlicesNoSubsets [][]string) {
	recipeNameSet := set.NewSet()
	for recipeName := range recipes {
		recipeNameSet.Add(recipeName)
	}

	recipeNamePowerSet := recipeNameSet.PowerSet()
	recipeNameMatchingSets := []set.Set{}
	for recipeNameSubsetInterface := range recipeNamePowerSet.Iter() {
		func() {
			remainingProducts := ProductMap{}
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

					if remainingProduct.MeasurementUnit == ingredient.MeasurementUnit {
						remainingProduct.Quantity -= ingredient.Quantity
					} else {
						log.Printf(`measurement units "%s" (from product list) and "%s" (from recipe) are incomparable`, remainingProduct.MeasurementUnit, ingredient.MeasurementUnit)
						continue
					}

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

	recipeNameMatchingSetsNoSubsets := []set.Set{}
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

	recipeNameMatchingSetSlicesNoSubsets = [][]string{}
	for _, recipeNameSubset := range recipeNameMatchingSetsNoSubsets {
		recipeNameSubsetSlice := []string{}
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

	var conversionTableCSVFilename string
	flag.StringVar(&conversionTableCSVFilename, "conversionTableCSV", "", "load a conversion table from a CSV file with the given name")

	var conversionTableINIFilename string
	flag.StringVar(&conversionTableINIFilename, "conversionTableINI", "", "load a conversion table from an INI file with the given name")

	var unitAliasTableFilename string
	flag.StringVar(&unitAliasTableFilename, "unitAliasTable", "", "load an alias table from an INI file with the given name")

	var productAliasTableFilename string
	flag.StringVar(&productAliasTableFilename, "productAliasTable", "", "load a product alias table from an INI file with the given name")

	flag.Parse()

	args := flag.Args()
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "not enough arguments")
		os.Exit(1)
	}

	unitConversionTable := ConversionTable{}
	baseConversionTable := BaseConversionTable{}
	unitAliasTable := UnitAliasTable{}
	baseUnitAliasTable := AliasTable{}
	productAliasTable := AliasTable{}
	if conversionTableCSVFilename != "" {
		loadConversionTableCSV(conversionTableCSVFilename, unitConversionTable, baseConversionTable)
	}
	if conversionTableINIFilename != "" {
		loadConversionTableINI(conversionTableINIFilename, unitConversionTable, baseConversionTable)
	}
	if unitAliasTableFilename != "" {
		loadUnitAliasTable(unitAliasTableFilename, unitAliasTable, baseUnitAliasTable)
	}
	if productAliasTableFilename != "" {
		loadProductAliasTable(productAliasTableFilename, productAliasTable)
	}

	recipeMetadataFile, err := os.Open(args[0])
	if err != nil {
		log.Fatal(err)
	}
	defer recipeMetadataFile.Close()

	recipeSources := RecipeSourceMap{}
	loadRecipeMetadata(recipeMetadataFile, recipeSources)

	recipes := RecipeMap{}
	for _, filename := range args[1:] {
		file, err := os.Open(filename)
		if err != nil {
			log.Fatal(err)
		}
		defer file.Close()

		importRecipesFromCSV(file, recipes)
	}

	for _, recipe := range recipes {
		for ingredientName, ingredient := range recipe {
			convertProductUnit(unitConversionTable, baseConversionTable, unitAliasTable, baseUnitAliasTable, productAliasTable, ingredient)
			if ingredient.Name != ingredientName {
				recipe[ingredient.Name] = ingredient
				delete(recipe, ingredientName)
			}
		}
	}

	http.HandleFunc("/products", func(w http.ResponseWriter, r *http.Request) {
		products := getSupportedProducts(recipes)

		productsJSON, err := json.Marshal(products)
		if err != nil {
			log.Fatal(err)
		}

		w.Header().Add("Content-Type", "application/json")
		if isDebugMode {
			w.Header().Add("Access-Control-Allow-Origin", "*")
		}
		w.Write(productsJSON)
	})

	http.HandleFunc("/recipes", func(w http.ResponseWriter, r *http.Request) {
		requestData, err := ioutil.ReadAll(r.Body)
		if err != nil {
			log.Fatal(err)
		}
		defer r.Body.Close()

		var request *RecipeSuggestionRequest
		err = json.Unmarshal(requestData, &request)
		if err != nil {
			log.Fatal(err)
		}

		for productName, product := range request.AvailableProducts {
			product.Name = productName
			convertProductUnit(unitConversionTable, baseConversionTable, unitAliasTable, baseUnitAliasTable, productAliasTable, product)
			if product.Name != productName {
				request.AvailableProducts[product.Name] = product
				delete(request.AvailableProducts, productName)
			}
		}

		if request.NumberOfServings > 1 {
			scaleRecipesByNumberOfServings(recipes, request.NumberOfServings)
		}

		possibleRecipeSets := getPossibleRecipeSets(request.AvailableProducts, recipes)
		for _, recipeNameSubsetSlice := range possibleRecipeSets {
			fmt.Println(strings.Join(recipeNameSubsetSlice, ", "))
		}

		possibleRecipeResponseSets := [][]*RecipeSuggestionResponse{}
		for _, possibleRecipeSet := range possibleRecipeSets {
			possibleRecipeResponseSet := []*RecipeSuggestionResponse{}
			for _, possibleRecipe := range possibleRecipeSet {
				recipeSource, ok := recipeSources[possibleRecipe]
				if !ok {
					log.Fatal("recipe not found")
				}

				possibleRecipeResponse := &RecipeSuggestionResponse{
					Name:   possibleRecipe,
					Source: recipeSource,
				}
				possibleRecipeResponseSet = append(possibleRecipeResponseSet, possibleRecipeResponse)
			}
			possibleRecipeResponseSets = append(possibleRecipeResponseSets, possibleRecipeResponseSet)
		}

		possibleRecipeResponseSetsJSON, err := json.Marshal(possibleRecipeResponseSets)
		if err != nil {
			log.Fatal(err)
		}

		w.Header().Add("Content-Type", "application/json")
		if isDebugMode {
			w.Header().Add("Access-Control-Allow-Origin", "*")
		}
		w.Write(possibleRecipeResponseSetsJSON)
	})

	log.Fatal(http.ListenAndServe(":8080", nil))
}
