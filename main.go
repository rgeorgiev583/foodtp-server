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

	set "github.com/deckarep/golang-set"
)

type Ingredient struct {
	Name            string
	Quantity        float64
	MeasurementUnit string
}

type IngredientMap map[string]*Ingredient
type RecipeMap map[string]IngredientMap

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

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "not enough arguments")
		os.Exit(1)
	}

	ingredientFile, err := os.Open(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}
	defer ingredientFile.Close()

	availableIngredients := IngredientMap{}
	importIngredientsFromCSV(ingredientFile, availableIngredients)

	recipes := RecipeMap{}
	for _, filename := range os.Args[2:] {
		file, err := os.Open(filename)
		if err != nil {
			log.Fatal(err)
		}
		defer file.Close()

		importRecipesFromCSV(file, recipes)
	}

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

					remainingIngredient.Quantity -= ingredient.Quantity
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

	for _, recipeNameSubset := range recipeNameMatchingSetsNoSubsets {
		recipeNameSubsetSlice := []string{}
		for recipeNameInterface := range recipeNameSubset.Iter() {
			recipeName := recipeNameInterface.(string)
			recipeNameSubsetSlice = append(recipeNameSubsetSlice, recipeName)
		}
		fmt.Println(strings.Join(recipeNameSubsetSlice, ", "))
	}
}
