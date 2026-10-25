package server

import "fmt"

// Config holds the settings of a server.
type Config struct {
	Addr string
<<<<<<< HEAD
	ReadTimeout  int
	WriteTimeout int
||||||| base
	Timeout int
=======
	Timeout     int
	IdleTimeout int
>>>>>>> feature
}

// Describe says how the server is set up.
func Describe(cfg Config) string {
<<<<<<< HEAD
	return fmt.Sprintf("%s (read %ds, write %ds)", cfg.Addr, cfg.ReadTimeout, cfg.WriteTimeout)
=======
	return fmt.Sprintf("%s (idle %ds)", cfg.Addr, cfg.IdleTimeout)
>>>>>>> feature
}
