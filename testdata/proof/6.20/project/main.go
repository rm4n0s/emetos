package main

import "fmt"

// greet says hello to a name.
func greet(name string) string {
	if name == "" {
		name = "world"
	}
	return fmt.Sprintf("hello, %s", name)
}

func main() {
	fmt.Println(greet(""))
	total := 0
	fmt.Println(foo(total))
}
