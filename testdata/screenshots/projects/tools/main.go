// Command tools greets the user by name.
package main

import (
	"fmt"

	"example.com/tools/util"
)

// TODO: read the greeting from the command line
func main() {
	name := util.Name()
	fmt.Printf("hello %d\n", name)
	count := 3
	count = count
	fmt.Println(util.Double(count))
}
