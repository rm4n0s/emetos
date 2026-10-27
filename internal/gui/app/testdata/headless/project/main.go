package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"time"
)

// Config is what the server reads from the environment.
type Config struct {
	Addr         string
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	Debug        bool
}

// FromEnv reads the configuration; a missing variable keeps its default.
func FromEnv() Config {
	cfg := Config{Addr: ":8080", ReadTimeout: 5 * time.Second, WriteTimeout: 10 * time.Second}
	if v := os.Getenv("ADDR"); v != "" {
		cfg.Addr = v
	}
	if v := os.Getenv("DEBUG"); v == "1" {
		cfg.Debug = true
	}
	return cfg
}

// Server wraps the HTTP server with the state the handlers share.
type Server struct {
	cfg     Config
	http    *http.Server
	started time.Time
	hits    map[string]int
}

// New makes a server for the configuration.
func New(cfg Config) *Server {
	s := &Server{cfg: cfg, hits: map[string]int{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.index)
	mux.HandleFunc("/health", s.health)
	s.http = &http.Server{Addr: cfg.Addr, Handler: mux, ReadTimeout: cfg.ReadTimeout, WriteTimeout: cfg.WriteTimeout}
	return s
}

func (s *Server) index(w http.ResponseWriter, r *http.Request) {
	s.hits[r.URL.Path]++
	fmt.Fprintf(w, "hello from %s, %d hits\n", r.URL.Path, s.hits[r.URL.Path])
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	fmt.Fprintf(w, "ok, up for %s\n", time.Since(s.started).Round(time.Second))
}

// Run serves until the context ends, then shuts down within five seconds.
func (s *Server) Run(ctx context.Context) error {
	s.started = time.Now()
	errs := make(chan error, 1)
	go func() { errs <- s.http.ListenAndServe() }()
	select {
	case err := <-errs:
		return err
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.http.Shutdown(shutdown)
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := New(FromEnv()).Run(ctx); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
