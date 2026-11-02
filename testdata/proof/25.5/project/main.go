package main

import (
	"fmt"

	"pricelist/src"
)

func main() {
	rows, err := src.Load("bread 2.5\ntea 4\njam 7.25")
	fmt.Println(rows, err)
}
