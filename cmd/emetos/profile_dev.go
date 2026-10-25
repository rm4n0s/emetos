//go:build emetos_dev

package main

import (
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strconv"
	"sync"
	"syscall"
)

// startProfiling lets a builder with the emetos_dev tag profile the running editor from the outside
// (docs/PERFORMANCE.md, "Profiling"). It does nothing unless EMETOS_PROFILE_DIR names a folder. Then:
//
//   - SIGUSR1 writes heap-N.pprof (live objects, after a garbage collection) and allocs-N.pprof
//     (everything allocated since the start) to the folder;
//   - SIGUSR2 starts a CPU profile, and the next SIGUSR2 stops it and writes cpu-N.pprof;
//   - EMETOS_PROFILE_START=cpu starts a CPU profile at once, at EMETOS_PROFILE_HZ samples a second
//     (1000 by default), for a profile of the start; the first SIGUSR2 then stops it;
//   - the goroutine profile and the memory statistics are written with the heap profile
//     (goroutines-N.txt, memstats-N.txt).
//
// The function it returns stops the handling and any CPU profile that is running.
func startProfiling(l *slog.Logger) (stop func()) {
	dir := os.Getenv("EMETOS_PROFILE_DIR")
	if dir == "" {
		return func() {}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		l.Error("profiling: cannot make the folder", "dir", dir, "err", err)
		return func() {}
	}
	sigs := make(chan os.Signal, 4)
	done := make(chan struct{})
	signal.Notify(sigs, syscall.SIGUSR1, syscall.SIGUSR2)
	var mu sync.Mutex // guards cpu: the signal goroutine and the stop function both end the profile
	var cpu *os.File
	cpuN, dumpN := 0, 0
	startCPU := func() {
		cpuN++
		f, err := os.Create(filepath.Join(dir, fmt.Sprintf("cpu-%d.pprof", cpuN)))
		if err != nil {
			l.Error("profiling: cannot make the CPU profile", "err", err)
			return
		}
		if err := pprof.StartCPUProfile(f); err != nil {
			l.Error("profiling: cannot start the CPU profile", "err", err)
			if cerr := f.Close(); cerr != nil {
				l.Error("profiling: cannot close the CPU profile", "err", cerr)
			}
			return
		}
		cpu = f
		l.Info("profiling: CPU profile started", "n", cpuN)
	}
	if os.Getenv("EMETOS_PROFILE_START") == "cpu" {
		hz := 1000
		if n, err := strconv.Atoi(os.Getenv("EMETOS_PROFILE_HZ")); err == nil && n > 0 {
			hz = n
		}
		runtime.SetCPUProfileRate(hz)
		mu.Lock()
		startCPU()
		mu.Unlock()
	}
	stopCPU := func() {
		mu.Lock()
		defer mu.Unlock()
		if cpu == nil {
			return
		}
		pprof.StopCPUProfile()
		if err := cpu.Close(); err != nil {
			l.Error("profiling: cannot close the CPU profile", "err", err)
		}
		cpu = nil
	}
	go func() {
		for {
			select {
			case <-done:
				return
			case sig := <-sigs:
				if sig == syscall.SIGUSR2 {
					mu.Lock()
					running := cpu != nil
					mu.Unlock()
					if running {
						stopCPU()
						l.Info("profiling: CPU profile written", "n", cpuN)
						continue
					}
					mu.Lock()
					startCPU()
					mu.Unlock()
					continue
				}
				dumpN++
				writeProfiles(l, dir, dumpN)
			}
		}
	}()
	return func() {
		signal.Stop(sigs)
		close(done)
		stopCPU()
	}
}

// writeProfiles writes the memory profiles and statistics, numbered n, to dir.
func writeProfiles(l *slog.Logger, dir string, n int) {
	runtime.GC()
	for _, p := range []struct{ name, file string }{
		{"heap", "heap"}, {"allocs", "allocs"}, {"goroutine", "goroutines"},
	} {
		ext := ".pprof"
		debug := 0
		if p.name == "goroutine" {
			ext, debug = ".txt", 1
		}
		f, err := os.Create(filepath.Join(dir, fmt.Sprintf("%s-%d%s", p.file, n, ext)))
		if err != nil {
			l.Error("profiling: cannot make a profile", "profile", p.name, "err", err)
			continue
		}
		if err := pprof.Lookup(p.name).WriteTo(f, debug); err != nil {
			l.Error("profiling: cannot write a profile", "profile", p.name, "err", err)
		}
		if err := f.Close(); err != nil {
			l.Error("profiling: cannot close a profile", "profile", p.name, "err", err)
		}
	}
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	text := fmt.Sprintf("HeapAlloc %d\nHeapSys %d\nHeapIdle %d\nHeapInuse %d\nHeapReleased %d\nStackSys %d\nMSpanSys %d\nMCacheSys %d\nBuckHashSys %d\nGCSys %d\nOtherSys %d\nSys %d\nNumGC %d\nTotalAlloc %d\nMallocs %d\n",
		m.HeapAlloc, m.HeapSys, m.HeapIdle, m.HeapInuse, m.HeapReleased, m.StackSys, m.MSpanSys, m.MCacheSys, m.BuckHashSys, m.GCSys, m.OtherSys, m.Sys, m.NumGC, m.TotalAlloc, m.Mallocs)
	if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("memstats-%d.txt", n)), []byte(text), 0o644); err != nil {
		l.Error("profiling: cannot write the memory statistics", "err", err)
	}
	l.Info("profiling: memory profiles written", "n", n)
}
