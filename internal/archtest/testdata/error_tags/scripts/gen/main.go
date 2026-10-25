package main

import "github.com/rm4n0s/errors"

// scripts is outside the folders the tags are taken from.
func main() { _ = errors.New("OutsideTheRoots", "not listed") }
