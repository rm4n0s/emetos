package main

import (
	"fmt"
	"strings"
)

//go:generate echo generated

// Point is a place on the grid.
type Point struct {
	X, Y int
}

// scale moves a point away from the origin.
func scale(p Point, factor int, label string) Point {
	fmt.Println(label)
	return Point{p.X * factor, p.Y * factor}
}

func main() {
	const limit = 3
	origin := Point{1, 2}
	moved := scale(origin, limit, "big")
	for i, name := range []string{"x", "y"} {
		fmt.Println(strings.Repeat(name, i+limit), moved)
	}
}
