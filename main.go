package main

import (
	"bufio"
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"os"
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
	Quantity        float64
	MeasurementUnit string
}

type IngredientMap map[string]*Ingredient
type RecipeMap map[string]IngredientMap

func loadConversionTable(filename string, conversionTable ConversionTable) {
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

func importIngredientsFromCSV(reader io.Reader, ingredients IngredientMap) {
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
		ingredientQuantityStr := ingredientRecord[1]
		var ingredientQuantity float64
		if ingredientQuantityStr != "-" {
			ingredientQuantity, err = strconv.ParseFloat(ingredientQuantityStr, 64)
			if err != nil {
				log.Fatal(err)
			}
		}

		ingredientName := ingredientRecord[0]
		ingredients[ingredientName] = &Ingredient{
			Name:            ingredientName,
			Quantity:        ingredientQuantity,
			MeasurementUnit: ingredientRecord[2],
		}
	}

	return
}

func convertIngredientUnits(unitConversionTable ConversionTable, ingredients IngredientMap) {
	for _, ingredient := range ingredients {
		ingredientUnitDefinition, ok := unitConversionTable[ingredient.MeasurementUnit]
		var ingredientUnitMeasurement *Measurement
		if ok {
			ingredientUnitMeasurement, ok = ingredientUnitDefinition[ingredient.Name]
		}
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

					ingredientUnitDefinition, ok := unitConversionTable[ingredient.MeasurementUnit]
					var ingredientUnitMeasurement *Measurement
					if ok {
						ingredientUnitMeasurement, ok = ingredientUnitDefinition[ingredient.Name]
					}

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
	if len(os.Args) < 4 {
		fmt.Fprintln(os.Stderr, "not enough arguments")
		os.Exit(1)
	}

	numberOfServings, err := strconv.Atoi(os.Args[3])
	if err != nil {
		log.Fatal(err)
	}
	if numberOfServings <= 0 {
		fmt.Fprintln(os.Stderr, "number of servings cannot be negative or zero")
	}

	unitConversionTable := ConversionTable{}
	loadConversionTable(os.Args[1], unitConversionTable)

	ingredientFile, err := os.Open(os.Args[2])
	if err != nil {
		log.Fatal(err)
	}
	defer ingredientFile.Close()

	availableIngredients := IngredientMap{}
	importIngredientsFromCSV(ingredientFile, availableIngredients)

	convertIngredientUnits(unitConversionTable, availableIngredients)

	recipes := RecipeMap{}
	for _, filename := range os.Args[4:] {
		file, err := os.Open(filename)
		if err != nil {
			log.Fatal(err)
		}
		defer file.Close()

		importRecipesFromCSV(file, recipes)
	}

	if numberOfServings > 1 {
		scaleRecipesByNumberOfServings(recipes, numberOfServings)
	}

	possibleRecipeSets := getPossibleRecipeSets(unitConversionTable, availableIngredients, recipes)
	for _, recipeNameSubsetSlice := range possibleRecipeSets {
		fmt.Println(strings.Join(recipeNameSubsetSlice, ", "))
	}
}
