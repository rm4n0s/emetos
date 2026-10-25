package main

import (
	"fmt"
	"log"
	"net/http"
)

// main serves the health check of the api.
func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, "ok")
	})
	log.Fatal(http.ListenAndServe(":8080", mux))
}
