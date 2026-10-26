package main

import (
	"fmt"
	"strings"
)

// Greeting joins the words of a greeting with a separator and ends it
// with an exclamation mark.
func Greeting(sep string, words ...string) string {
	return strings.Join(words, sep) + "!"
}

func main() {
	fmt.Println(Greeting(", ", "hello", "world"))
}
