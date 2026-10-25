// Command proof is a tiny program the proofs of step 11.5 look at.
package main

import (
	"fmt"

	"example.com/proof/util"
)

// TODO: read the greeting from the command line
func main() {
	name := util.Name()
	fmt.Printf("hello %d\n", name)
	count := 3
	count = count
	fmt.Println(util.Double(count))
}
