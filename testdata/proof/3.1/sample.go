// Command sample greets the people named on the command line.
package main

import (
	"fmt"
	"os"
	"strings"
)

// greeting is the text before each name.
const greeting = "Hello"

func main() {
	names := os.Args[1:]
	if len(names) == 0 {
		names = []string{"world"}
	}
	for i, name := range names {
		fmt.Printf("%d. %s, %s!\n", i+1, greeting, strings.Title(name))
	}
}
