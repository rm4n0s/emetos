package redis

import (
	"strings"
	"testing"
)

func args(line string) []string { return strings.Fields(line) }

func TestDangerousCommandsAskFirst(t *testing.T) {
	tests := []struct {
		line string
		ask  bool
	}{
		{"FLUSHDB", true}, {"flushdb async", true}, {"FLUSHALL", true}, {"flushall SYNC", true}, {"SHUTDOWN NOSAVE", true},
		{"CONFIG SET maxmemory 1gb", true}, {"config set appendonly yes", true}, {"DEBUG SLEEP 0", true}, {"debug", true},
		{"KEYS *", true}, {"keys user:*", true}, {"SCRIPT FLUSH", true}, {"script flush async", true}, {"CLIENT KILL ID 3", true},
		{"client kill 127.0.0.1:1234", true}, {"DEL a b", true}, {"del a b c", true},
		{"DEL a", false}, {"del", false}, {"GET a", false}, {"CONFIG GET maxmemory", false}, {"CONFIG", false}, {"SCRIPT LOAD x", false},
		{"CLIENT LIST", false}, {"SCAN 0", false}, {"UNLINK a b", false}, {"FLUSHDBX", false}, {"", false},
	}
	for _, tc := range tests {
		t.Run(tc.line, func(t *testing.T) {
			why := Dangerous(args(tc.line))
			if (why != "") != tc.ask {
				t.Errorf("Dangerous(%q) = %q, ask = %v", tc.line, why, tc.ask)
			}
		})
	}
}

func TestSomeCommandsAreNotSentByTheConsole(t *testing.T) {
	tests := []struct {
		line   string
		refuse bool
	}{
		{"SUBSCRIBE a", true}, {"psubscribe a*", true}, {"MONITOR", true}, {"MULTI", true}, {"EXEC", true}, {"watch k", true}, {"HELLO 3", true},
		{"AUTH x", true}, {"QUIT", true}, {"CLIENT SETNAME x", true}, {"client reply off", true}, {"RESET", true},
		{"GET a", false}, {"SELECT 3", false}, {"CLIENT LIST", false}, {"PUBLISH a b", false}, {"INFO", false}, {"", false},
	}
	for _, tc := range tests {
		t.Run(tc.line, func(t *testing.T) {
			if why := Unsupported(args(tc.line)); (why != "") != tc.refuse {
				t.Errorf("Unsupported(%q) = %q", tc.line, why)
			}
		})
	}
}

func TestTheBuiltInListsOfAReadOnlyConnection(t *testing.T) {
	tests := []struct {
		line   string
		refuse bool
	}{
		{"SET k v", true}, {"set k v", true}, {"DEL k", true}, {"HSET h f v", true}, {"LPUSH l a", true}, {"XADD s * a b", true},
		{"EXPIRE k 10", true}, {"RENAME a b", true}, {"JSON.SET k $ {}", true}, {"FLUSHALL", true}, {"PUBLISH c m", true}, {"EVAL x 0", true},
		{"CONFIG SET a b", true}, {"CLIENT KILL ID 1", true}, {"SCRIPT FLUSH", true}, {"XGROUP CREATE s g $", true}, {"ACL SETUSER x", true},
		{"SHUTDOWN", true}, {"DEBUG SLEEP 0", true}, {"MODULE LOAD x", true}, {"BGSAVE", true},
		{"GET k", false}, {"HGETALL h", false}, {"LRANGE l 0 -1", false}, {"KEYS *", false}, {"INFO", false}, {"CONFIG GET *", false},
		{"CLIENT LIST", false}, {"SCAN 0", false}, {"SORT_RO l", false}, {"PING", false}, {"XINFO STREAM s", false}, {"XRANGE s - +", false},
		{"SLOWLOG GET", false}, {"MEMORY STATS", false}, {"", false},
	}
	for _, tc := range tests {
		t.Run(tc.line, func(t *testing.T) {
			if got := RefusedByBuiltinList(args(tc.line)); got != tc.refuse {
				t.Errorf("RefusedByBuiltinList(%q) = %v, want %v", tc.line, got, tc.refuse)
			}
		})
	}
}
