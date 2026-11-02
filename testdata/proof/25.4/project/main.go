package main

import "fmt"

type Order struct {
	ID    int
	Total float64
	Items []string
}

func total(prices []float64) float64 {
	sum := 0.0
	for _, p := range prices {
		sum += p
	}
	return sum
}

func main() {
	prices := []float64{4.5, 12, 7.25}
	order := Order{ID: 42, Items: []string{"tea", "bread", "jam"}}
	order.Total = total(prices)
	fmt.Println("order", order.ID, "costs", order.Total)
}
