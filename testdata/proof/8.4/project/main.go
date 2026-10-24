package main

import (
	"fmt"

	"example.com/notes/internal/app"
)

func main() {
	a := app.New("notes")
	fmt.Println(a.Greeting())
}
