// Package greet says hello to people.
package main

import "fmt"

// greeting is the text that every greeting starts with.
const greeting = "hello"

// Greet says hello to a name.
func Greet(name string) string {
	if name == "" {
		name = "world"
	}
	return fmt.Sprintf("%s, %s", greeting, name)
}

func main() {
	fmt.Println(Greet(""))
}
