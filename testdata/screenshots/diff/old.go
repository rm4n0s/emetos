package server

import (
	"fmt"
	"log"
	"net"
	"time"
)

// Config holds the settings of a server.
type Config struct {
	Addr    string
	Timeout time.Duration
	Verbose bool
}

// Server accepts connections and answers each of them.
type Server struct {
	cfg      Config
	listener net.Listener
	served   int
}

// New makes a server for a configuration.
func New(cfg Config) *Server {
	return &Server{cfg: cfg}
}

// Listen starts to listen on the address of the configuration.
func (s *Server) Listen() error {
	l, err := net.Listen("tcp", s.cfg.Addr)
	if err != nil {
		return err
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
		s.served++
		go s.handle(conn)
	}
}

func (s *Server) handle(conn net.Conn) {
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(s.cfg.Timeout))
	fmt.Fprintf(conn, "hello, you are number %d\n", s.served)
}

// debugf prints a message when the server is verbose.
func (s *Server) debugf(format string, args ...any) {
	if s.cfg.Verbose {
		log.Printf(format, args...)
	}
}

// Close stops the server.
func (s *Server) Close() error {
	return s.listener.Close()
}
