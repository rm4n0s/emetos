package main

func testOptions() options {
	o := options{TestDevCommands: true}
	o.TestDevCommands = false
	return o
}
