package main

import "fmt"

// greeting is what the host machine prints.
func greeting(name string) string {
	return "Hello, " + name + "! This file is open on the other machine."
}

func main() {
	fmt.Println(greeting("Remote Work"))
}
