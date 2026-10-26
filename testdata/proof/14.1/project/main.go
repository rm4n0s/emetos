package main

import (
	"fmt"
	"strings"
)

// Greeter says hello to people.
type Greeter struct {
	Name string
}

// Greet returns the greeting for name, from the Greeter.
func (g Greeter) Greet(name string) string {
	return fmt.Sprintf("Hello, %s! I am %s.", name, g.Name)
}

func main() {
	g := Greeter{Name: "Emetos"}
	var sb strings.Builder
	sb.WriteString(g.Greet("Go"))
	fmt.Println(sb.String())
	sb
}
