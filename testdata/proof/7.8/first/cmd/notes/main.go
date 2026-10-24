package main

import (
	"fmt"
	"os"

	"example.com/notes/internal/store"
)

func main() {
	s := store.New()
	for _, arg := range os.Args[1:] {
		s.Add(arg)
	}
	for i, note := range s.All() {
		fmt.Printf("%d. %s\n", i+1, note)
	}
}
