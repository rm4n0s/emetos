package main

import (
	"fmt"
	"log"
	"net/http"
	"os"

	"example.com/notes/internal/server"
)

func main() {
	addr := os.Getenv("NOTES_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	srv := server.New(addr)
	fmt.Println("listening on", addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
