package server

import (
	"fmt"
	"log"
	"net"
	"sync/atomic"
	"time"
)

// Config holds the settings of a server.
type Config struct {
	Addr         string
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	Verbose      bool
}

// Server accepts connections and answers each of them.
type Server struct {
	cfg      Config
	listener net.Listener
	served   atomic.Int64
}

// New makes a server for a configuration.
func New(cfg Config) *Server {
	return &Server{cfg: cfg}
}

// Listen starts to listen on the address of the configuration.
func (s *Server) Listen() error {
	l, err := net.Listen("tcp", s.cfg.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", s.cfg.Addr, err)
	}
	s.listener = l
	log.Println("listening on", s.cfg.Addr)
	return nil
}

// Serve answers connections until the listener is closed.
func (s *Server) Serve() error {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return err
		}
		n := s.served.Add(1)
		go s.handle(conn, n)
	}
}

func (s *Server) handle(conn net.Conn, n int64) {
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(s.cfg.ReadTimeout))
	conn.SetWriteDeadline(time.Now().Add(s.cfg.WriteTimeout))
	fmt.Fprintf(conn, "hello, you are number %d\n", n)
}

// Served is how many connections the server has answered.
func (s *Server) Served() int64 {
	return s.served.Load()
}

// Close stops the server.
func (s *Server) Close() error {
	return s.listener.Close()
}
