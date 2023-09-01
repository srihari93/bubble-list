package main

import (
	"math/rand"
	"sync"
)

type randomItemGenerator struct {
	titles     []string
	titleIndex int
	mtx        *sync.Mutex
	shuffle    *sync.Once
}

func (r *randomItemGenerator) reset() {
	r.mtx = &sync.Mutex{}
	r.shuffle = &sync.Once{}

	r.titles = []string{
		"Artichoke",
		"Baking Flour",
		"Bananas",
		"Barley",
		"Bean Sprouts",
		"Bitter Melon",
		"Black Cod",
		"Blood Orange",
		"Brown Sugar",
		"Cashew Apple",
		"Cashews",
		"Cat Food",
		"Coconut Milk",
		"Cucumber",
		"Curry Paste",
		"Currywurst",
		"Dill",
		"Dragonfruit",
		"Dried Shrimp",
		"Eggs",
		"Fish Cake",
		"Furikake",
		"Garlic",
		"Gherkin",
		"Ginger",
		"Granulated Sugar",
		"Grapefruit",
		"Green Onion",
		"Hazelnuts",
		"Heavy whipping cream",
		"Honey Dew",
		"Horseradish",
		"Jicama",
		"Kohlrabi",
		"Leeks",
		"Lentils",
		"Licorice Root",
		"Meyer Lemons",
		"Milk",
		"Molasses",
		"Muesli",
		"Nectarine",
		"Niagamo Root",
		"Nopal",
		"Nutella",
		"Oat Milk",
		"Oatmeal",
		"Olives",
		"Papaya",
		"Party Gherkin",
		"Peppers",
		"Persian Lemons",
		"Pickle",
		"Pineapple",
		"Plantains",
		"Pocky",
		"Powdered Sugar",
		"Quince",
		"Radish",
		"Ramps",
		"Star Anise",
		"Sweet Potato",
		"Tamarind",
		"Unsalted Butter",
		"Watermelon",
		"Weißwurst",
		"Yams",
		"Yeast",
		"Yuzu",
		"Snow Peas",
	}

	r.shuffle.Do(func() {
		shuf := func(x []string) {
			rand.Shuffle(len(x), func(i, j int) { x[i], x[j] = x[j], x[i] })
		}
		shuf(r.titles)
	})
}

func (r *randomItemGenerator) next() item {
	if r.mtx == nil {
		r.reset()
	}

	r.mtx.Lock()
	defer r.mtx.Unlock()

	i := item{
		title: r.titles[r.titleIndex],
	}

	r.titleIndex++
	if r.titleIndex >= len(r.titles) {
		r.titleIndex = 0
	}

	return i
}
