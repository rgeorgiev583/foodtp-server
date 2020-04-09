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

type CulinaryUnitDefinition map[string]*Measurement
type ConversionTable map[string]CulinaryUnitDefinition

type Ingredient struct {
	Name            string
	Quantity        float64 `json:"quantity"`
	MeasurementUnit string  `json:"unit"`
}

type IngredientMap map[string]*Ingredient
type RecipeMap map[string]IngredientMap
type RecipeSourceMap map[string]string

type RecipeSuggestionRequest struct {
	NumberOfServings     int           `json:"numberOfServings"`
	AvailableIngredients IngredientMap `json:"products"`
}

type RecipeSuggestionResponse struct {
	Name   string `json:"name"`
	Source string `json:"source"`
}

func loadConversionTableCSV(filename string, conversionTable ConversionTable) {
	file, err := os.Open(filename)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	csvReader := csv.NewReader(file)
	ingredientRecords, err := csvReader.ReadAll()
	if err != nil {
		log.Fatal(err)
	}

	culinaryUnitDescriptions := ingredientRecords[0][1:]
	culinaryUnitCount := len(culinaryUnitDescriptions)
	culinaryUnits := make([]string, culinaryUnitCount, culinaryUnitCount)

	culinaryUnitDescriptionPattern := regexp.MustCompile(`(.+?)\s*\(\d+\s*.+\)`)
	for i, culinaryUnitDescription := range culinaryUnitDescriptions {
		culinaryUnitDescriptionMatch := culinaryUnitDescriptionPattern.FindStringSubmatch(culinaryUnitDescription)
		if len(culinaryUnitDescriptionMatch) < 2 {
			log.Print("error: invalid format of culinary unit description")
			return
		}
		if err != nil {
			log.Fatal(err)
		}

		culinaryUnits[i] = culinaryUnitDescriptionMatch[1]
	}

	for _, ingredientRecord := range ingredientRecords[1:] {
		for i, measurementStr := range ingredientRecord[1:] {
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
				unitDefinition = CulinaryUnitDefinition{}
				conversionTable[culinaryUnits[i]] = unitDefinition
			}

			unitDefinition[ingredientRecord[0]] = measurement
		}
	}
}

func loadConversionTableINI(filename string, conversionTable ConversionTable) {
	file, err := ini.Load(filename)
	if err != nil {
		log.Fatal(err)
	}

	for _, section := range file.Sections() {
		unitDefinition := CulinaryUnitDefinition{}

		for _, key := range section.Keys() {
			measurement := &Measurement{}
			_, err = fmt.Sscanln(key.Value(), &measurement.Quantity, &measurement.Unit)
			if err != nil {
				log.Fatal(err)
			}

			unitDefinition[key.Name()] = measurement
		}

		conversionTable[section.Name()] = unitDefinition
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

func getIngredientUnitMeasurement(unitConversionTable ConversionTable, ingredient *Ingredient) (ingredientUnitMeasurement *Measurement) {
	ingredientUnitDefinition, ok := unitConversionTable[ingredient.MeasurementUnit]
	if ok {
		ingredientUnitMeasurement, ok = ingredientUnitDefinition[ingredient.Name]
	}
	return
}

func convertIngredientUnits(unitConversionTable ConversionTable, ingredients IngredientMap) {
	for _, ingredient := range ingredients {
		ingredientUnitMeasurement := getIngredientUnitMeasurement(unitConversionTable, ingredient)
		if ingredientUnitMeasurement != nil {
			ingredient.MeasurementUnit = ingredientUnitMeasurement.Unit
			ingredient.Quantity *= ingredientUnitMeasurement.Quantity
		}
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
			recipe = IngredientMap{}
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

		recipe[ingredientRecord[0]] = &Ingredient{
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

func getPossibleRecipeSets(unitConversionTable ConversionTable, availableIngredients IngredientMap, recipes RecipeMap) (recipeNameMatchingSetSlicesNoSubsets [][]string) {
	recipeNameSet := set.NewSet()
	for recipeName := range recipes {
		recipeNameSet.Add(recipeName)
	}

	recipeNamePowerSet := recipeNameSet.PowerSet()
	recipeNameMatchingSets := []set.Set{}
	for recipeNameSubsetInterface := range recipeNamePowerSet.Iter() {
		func() {
			remainingIngredients := IngredientMap{}
			for ingredientName, ingredient := range availableIngredients {
				ingredientCopy := *ingredient
				remainingIngredients[ingredientName] = &ingredientCopy
			}
			recipeNameSubset := recipeNameSubsetInterface.(set.Set)
			for recipeNameInterface := range recipeNameSubset.Iter() {
				recipeName := recipeNameInterface.(string)
				recipe, _ := recipes[recipeName]
				for _, ingredient := range recipe {
					remainingIngredient, ok := remainingIngredients[ingredient.Name]
					if !ok {
						return
					}

					ingredientUnitMeasurement := getIngredientUnitMeasurement(unitConversionTable, ingredient)
					if ingredientUnitMeasurement != nil && remainingIngredient.MeasurementUnit == ingredientUnitMeasurement.Unit {
						remainingIngredient.Quantity -= ingredient.Quantity * ingredientUnitMeasurement.Quantity
					} else if remainingIngredient.MeasurementUnit != ingredient.MeasurementUnit {
						log.Printf(`measurement units "%s" (from product list) and "%s" (from recipe) are incomparable`, remainingIngredient.MeasurementUnit, ingredient.MeasurementUnit)
						continue
					} else {
						remainingIngredient.Quantity -= ingredient.Quantity
					}

					if remainingIngredient.MeasurementUnit != "на вкус" && remainingIngredient.Quantity < 0 {
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

	flag.Parse()

	args := flag.Args()
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "not enough arguments")
		os.Exit(1)
	}

	unitConversionTable := ConversionTable{}
	if conversionTableCSVFilename != "" {
		loadConversionTableCSV(conversionTableCSVFilename, unitConversionTable)
	}
	if conversionTableINIFilename != "" {
		loadConversionTableINI(conversionTableINIFilename, unitConversionTable)
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

		for ingredientName, ingredient := range request.AvailableIngredients {
			ingredient.Name = ingredientName
		}

		convertIngredientUnits(unitConversionTable, request.AvailableIngredients)

		if request.NumberOfServings > 1 {
			scaleRecipesByNumberOfServings(recipes, request.NumberOfServings)
		}

		possibleRecipeSets := getPossibleRecipeSets(unitConversionTable, request.AvailableIngredients, recipes)
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
