package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// Note is one line of the notes file.
type Note struct {
	Line int
	Text string
}

// read gives the notes of a file, the blank lines left out.
func read(path string) ([]Note, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var notes []Note
	sc := bufio.NewScanner(f)
	for n := 1; sc.Scan(); n++ {
		if text := strings.TrimSpace(sc.Text()); text != "" {
			notes = append(notes, Note{Line: n, Text: text})
		}
	}
	return notes, sc.Err()
}

func main() {
	notes, err := read("docs/todo.txt")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	for _, n := range notes {
		fmt.Printf("%3d  %s\n", n.Line, n.Text)
	}
}
