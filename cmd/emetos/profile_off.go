//go:build !emetos_dev

package main

import "log/slog"

// startProfiling does nothing without the emetos_dev tag (profile_dev.go).
func startProfiling(*slog.Logger) (stop func()) { return func() {} }
