package main

import (
	"os"
	"strconv"
	"testing"
	"time"
)

// TestSleep writes its process id to the file in EMETOS_PIDFILE, then sleeps for a minute.
func TestSleep(t *testing.T) {
	if f := os.Getenv("EMETOS_PIDFILE"); f != "" {
		if err := os.WriteFile(f, []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	time.Sleep(time.Minute)
}
