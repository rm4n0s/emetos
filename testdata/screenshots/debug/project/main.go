package main

import "fmt"

type Order struct {
	ID    int
	Items []string
	Total float64
}

func discount(order Order, rate float64) float64 {
	amount := order.Total * rate
	return amount
}

func main() {
	order := Order{ID: 7, Items: []string{"keyboard", "mouse"}, Total: 49.5}
	saved := discount(order, 0.1)
	fmt.Printf("order %d saves %.2f\n", order.ID, saved)
}
