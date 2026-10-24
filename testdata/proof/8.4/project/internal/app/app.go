// Package app holds the notes program.
package app

// App is the notes program.
type App struct {
	name string
}

// New makes the program with a name.
func New(name string) *App { return &App{name: name} }

// Greeting is the first line the program prints.
func (a *App) Greeting() string { return "Welcome to " + a.name }
