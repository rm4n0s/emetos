package main

import "strings"

// Shout makes a text loud.
func Shout(text string) string {
	return strings.ToUpper(text) + "!"
}

// Whisper makes a text quiet.
func Whisper(text string) string {
	return strings.ToLower(text) + "..."
}
