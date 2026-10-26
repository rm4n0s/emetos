package main

import "strings"

// Greet builds the greeting for a name.
func Greet(name string) string {
	return "hello, " + strings.TrimSpace(name)
}

// Shout is Greet in capitals.
func Shout(name string) string {
	return strings.ToUpper(Greet(name))
}
