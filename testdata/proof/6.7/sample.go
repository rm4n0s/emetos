// Command sample shows the window the menus belong to.
package main

import (
	"fmt"
	"strings"
)

// items are the words the program joins.
var items = []string{"edit", "selection", "view", "go"}

func main() {
	fmt.Println(strings.Join(items, " | "))
}
