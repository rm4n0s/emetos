package main

type options struct{ TestDevCommands bool }

func run() bool {
	o := options{TestDevCommands: true}
	var p options
	p.TestDevCommands = true
	return o.TestDevCommands && p.TestDevCommands
}
