// Package greeter says hello in a few languages.
package greeter

import (
	"fmt"
	"strings"
)

// Greeting is a hello in one language.
type Greeting struct {
	Language string
	Text     string
}

// All returns every greeting the package knows.
func All() []Greeting {
	return []Greeting{
		{Language: "English", Text: "Hello"},
		{Language: "Greek", Text: "Γεια σου"},
		{Language: "Japanese", Text: "こんにちは"},
	}
}

// Say writes the greeting for a language, or an apology.
func Say(language, name string) string {
	for _, g := range All() {
		if strings.EqualFold(g.Language, language) {
			return fmt.Sprintf("%s, %s!", g.Text, name)
		}
	}
	return fmt.Sprintf("Sorry %s, no greeting in %s", name, language)
}

// Languages lists the languages that have a greeting.
func Languages() []string {
	var names []string
	for _, g := range All() {
		names = append(names, g.Language)
	}
	return names
}
