// Package server holds the handlers of the api.
package server

import "net/http"

// Health answers the health check.
func Health(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}
