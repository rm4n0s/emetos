package main

import "fmt"

func main() {
	names := []string{"ada", "grace", "linus"}
	for _, n := range names {
		fmt.Println(Greet(n))
	}
	fmt.Println(Shout(Greet("everyone")))
}
