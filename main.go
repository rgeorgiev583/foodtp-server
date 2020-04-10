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
type AliasMap map[string]string
type UnitAliasTable map[string]AliasMap

type Product struct {
	Name            string
	Quantity        float64 `json:"quantity"`
	MeasurementUnit string  `json:"unit"`
}

type ProductMap map[string]*Product
type RecipeTable map[string]ProductMap
type RecipeSourceMap map[string]string

type RecipeSuggestionRequest struct {
	NumberOfServings  int        `json:"numberOfServings"`
	AvailableProducts ProductMap `json:"products"`
}

type RecipeSuggestionResponse struct {
	Name   string `json:"name"`
	Source string `json:"source"`
}

func convertStringSetToSortedSlice(set StringSet) (slice []string) {
	slice = make([]string, 0, len(set))
	for product := range set {
		slice = append(slice, product)
	}
	sort.Strings(slice)
	return
}

func loadConversionTableCSV(filename string, conversionTable ConversionTable, baseConversionMap BaseConversionMap) {
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
		baseConversionMap[culinaryUnit] = &Measurement{
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
				unitDefinition = BaseConversionMap{}
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

func loadConversionTableINI(filename string, conversionTable ConversionTable, baseConversionMap BaseConversionMap) {
	file, err := ini.Load(filename)
	if err != nil {
		log.Fatal(err)
	}

	baseUnitDefinitions, err := file.GetSection("DEFAULT")
	if err != nil {
		log.Fatal(err)
	}
	for _, key := range baseUnitDefinitions.Keys() {
		baseConversionMap[key.Name()] = getMeasurement(key.Value())
	}

	for _, section := range file.Sections() {
		unitDefinition := BaseConversionMap{}

		for _, key := range section.Keys() {
			unitDefinition[key.Name()] = getMeasurement(key.Value())
		}

		conversionTable[section.Name()] = unitDefinition
	}
}

func loadUnitAliasTable(filename string, unitAliasTable UnitAliasTable, baseUnitAliasMap AliasMap) {
	file, err := ini.Load(filename)
	if err != nil {
		log.Fatal(err)
	}

	baseUnitDefinitions, err := file.GetSection("DEFAULT")
	if err != nil {
		log.Fatal(err)
	}
	for _, key := range baseUnitDefinitions.Keys() {
		baseUnitAliasMap[key.Name()] = key.Value()
	}

	for _, section := range file.Sections() {
		unitAliasDefinition := AliasMap{}

		for _, key := range section.Keys() {
			unitAliasDefinition[key.Name()] = key.Value()
		}

		unitAliasTable[section.Name()] = unitAliasDefinition
	}
}

func loadProductAliasMap(filename string, productAliasMap AliasMap) {
	file, err := ini.Load(filename)
	if err != nil {
		log.Fatal(err)
	}

	section, err := file.GetSection("DEFAULT")
	if err != nil {
		log.Fatal(err)
	}
	for _, key := range section.Keys() {
		productAliasMap[key.Name()] = key.Value()
	}
}

func loadRecipeMetadata(filename string, recipeSources RecipeSourceMap) {
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

func convertProductUnit(unitConversionTable ConversionTable, baseConversionMap BaseConversionMap, unitAliasTable UnitAliasTable, baseUnitAliasMap AliasMap, productAliasMap AliasMap, product *Product) {
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
		productUnitMeasurement, ok = baseConversionMap[product.MeasurementUnit]
	}
	if productUnitMeasurement != nil {
		product.MeasurementUnit = productUnitMeasurement.Unit
		product.Quantity *= productUnitMeasurement.Quantity
	}
}

func importRecipesFromCSV(filename string, recipes RecipeTable, products StringSet) {
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
		products[ingredientRecord[0]] = struct{}{}
	}

	return
}

func scaleRecipesByNumberOfServings(recipes RecipeTable, numberOfServings int) {
	for _, recipe := range recipes {
		for _, ingredient := range recipe {
			ingredient.Quantity *= 2
		}
	}
}

func getPossibleRecipeSets(availableProducts ProductMap, recipes RecipeTable) (recipeNameMatchingSetSlicesNoSubsets [][]string) {
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

	var productAliasMapFilename string
	flag.StringVar(&productAliasMapFilename, "productAliasMap", "", "load a product alias map from an INI file with the given name")

	flag.Parse()

	args := flag.Args()
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "not enough arguments")
		os.Exit(1)
	}

	unitConversionTable := ConversionTable{}
	baseConversionMap := BaseConversionMap{}
	unitAliasTable := UnitAliasTable{}
	baseUnitAliasMap := AliasMap{}
	productAliasMap := AliasMap{}
	if conversionTableCSVFilename != "" {
		loadConversionTableCSV(conversionTableCSVFilename, unitConversionTable, baseConversionMap)
	}
	if conversionTableINIFilename != "" {
		loadConversionTableINI(conversionTableINIFilename, unitConversionTable, baseConversionMap)
	}
	if unitAliasTableFilename != "" {
		loadUnitAliasTable(unitAliasTableFilename, unitAliasTable, baseUnitAliasMap)
	}
	if productAliasMapFilename != "" {
		loadProductAliasMap(productAliasMapFilename, productAliasMap)
	}

	recipeSources := RecipeSourceMap{}
	loadRecipeMetadata(args[0], recipeSources)

	recipes := RecipeTable{}
	productSet := StringSet{}
	for _, filename := range args[1:] {
		importRecipesFromCSV(filename, recipes, productSet)
	}
	products := convertStringSetToSortedSlice(productSet)

	for _, recipe := range recipes {
		for ingredientName, ingredient := range recipe {
			convertProductUnit(unitConversionTable, baseConversionMap, unitAliasTable, baseUnitAliasMap, productAliasMap, ingredient)
			if ingredient.Name != ingredientName {
				recipe[ingredient.Name] = ingredient
				delete(recipe, ingredientName)
			}
		}
	}

	http.HandleFunc("/products", func(w http.ResponseWriter, r *http.Request) {
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
			convertProductUnit(unitConversionTable, baseConversionMap, unitAliasTable, baseUnitAliasMap, productAliasMap, product)
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
