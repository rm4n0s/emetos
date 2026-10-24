// Package server holds the HTTP server of the notes.
package server

import "net/http"

// New makes the server that listens on addr.
func New(addr string) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	return &http.Server{Addr: addr, Handler: mux}
}
