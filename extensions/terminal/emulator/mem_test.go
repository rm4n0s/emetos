package emulator

import "runtime"

type runtimeMem struct{ heap uint64 }

// read stores the live heap after a collection.
func (m *runtimeMem) read() {
	runtime.GC()
	runtime.GC()
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	m.heap = ms.HeapAlloc
}

func runtimeKeepAlive(x any) { runtime.KeepAlive(x) }
