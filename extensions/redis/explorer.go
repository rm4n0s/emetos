package redis

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rm4n0s/emetos/extensions/redis/keytree"
)

// The kinds of node the explorer lists (dbconn.Node.Kind).
const (
	kindDB     = "db"
	kindFolder = "folder"
	kindKey    = "key"
	kindMore   = "more"
	kindEmpty  = "empty"
)

// DefaultScanCount is the number of keys a page of the explorer asks SCAN for (the setting redis.scanCount).
const DefaultScanCount = 500

// maxScanCalls bounds the SCAN calls that make one page of keys: a pattern that matches little may need many to find 500 keys, and
// the user can ask for the next page.
const maxScanCalls = 100

// nodeData is the Data of the nodes the explorer makes.
type nodeData struct {
	Kind string
	DB   int
	// Path is the prefix of a folder; Key, Type and TTLMillis describe a key.
	Path      string
	Key       string
	Type      string
	TTLMillis int64
}

// dbState is what the explorer holds of one database of a connection: the keys scanned so far, the tree they make and where the
// scan stands. mu is held for a whole page load, so two requests do not load the same page twice.
type dbState struct {
	index int
	db    DB
	sep   string

	// fmu guards filter alone, so that the list of databases can read it while a page of keys is being loaded under mu.
	fmu    sync.Mutex
	filter string

	mu      sync.Mutex
	pattern string
	tree    *keytree.Tree
	cur     Cursor
	started bool
	// size is DBSIZE when the scan started; the description of the "Load more" row compares the keys loaded with it.
	size int64
}

func newDBState(index int, db DB, sep string) *dbState {
	return &dbState{index: index, db: db, sep: sep, tree: keytree.New(sep)}
}

// reset forgets the keys, so that the next load scans again from the start. The caller holds mu.
func (st *dbState) reset() {
	st.tree = keytree.New(st.sep)
	st.cur, st.started, st.size = Cursor{}, false, 0
}

// Pattern turns what the user typed in the filter into a MATCH pattern: a text with a glob character (* ? [ ] \) is the pattern, any
// other text matches the keys that contain it.
func Pattern(filter string) string {
	filter = strings.TrimSpace(filter)
	if filter == "" {
		return ""
	}
	if strings.ContainsAny(filter, `*?[]\`) {
		return filter
	}
	return "*" + filter + "*"
}

// loadPage scans until it has a page of keys (want) or the scan is over, or maxScanCalls calls were made, then asks the type and TTL
// of the keys and puts them in the tree. The page is committed only when all of it was read. The caller holds mu.
func (st *dbState) loadPage(ctx context.Context, count int64) error {
	if !st.started {
		size, err := st.db.DBSize(ctx)
		if err != nil {
			return err
		}
		st.size = size
	}
	cur := st.cur
	var keys []string
	for calls := 0; calls < maxScanCalls && int64(len(keys)) < count && !cur.Done(); calls++ {
		got, next, err := st.db.Scan(ctx, cur, st.pattern, count)
		if err != nil {
			return err
		}
		keys = append(keys, got...)
		cur = next
	}
	metas, err := st.db.Metas(ctx, keys)
	if err != nil {
		return err
	}
	leaves := make([]keytree.Leaf, 0, len(keys))
	for i, k := range keys {
		if metas[i].Type == TypeNone {
			continue // deleted between the scan and the question
		}
		leaves = append(leaves, keytree.Leaf{Key: k, Type: metas[i].Type, TTLMillis: metas[i].TTLMillis})
	}
	st.tree.Add(leaves...)
	st.cur, st.started = cur, true
	return nil
}

// ensureLoaded loads the first page when nothing was loaded yet.
func (st *dbState) ensureLoaded(ctx context.Context, count int64) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.started {
		return nil
	}
	return st.loadPage(ctx, count)
}

// loadMore loads the next page.
func (st *dbState) loadMore(ctx context.Context, count int64) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.started && st.cur.Done() {
		return nil
	}
	return st.loadPage(ctx, count)
}

// setFilter changes the filter and forgets the keys.
func (st *dbState) setFilter(filter string) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.fmu.Lock()
	st.filter = strings.TrimSpace(filter)
	st.fmu.Unlock()
	st.pattern = Pattern(filter)
	st.reset()
}

// filterText is the filter the user set.
func (st *dbState) filterText() string {
	st.fmu.Lock()
	defer st.fmu.Unlock()
	return st.filter
}

// refresh forgets the keys, keeping the filter.
func (st *dbState) refresh() {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.reset()
}

// forgetKey takes a key out of the tree (it was deleted or renamed here).
func (st *dbState) forgetKey(key string) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.tree.Remove(key)
}

// addKey puts a key in the tree, with what the server says of it, when it matches the filter's idea of the tree (the key is shown
// even if the pattern would not have matched it: the user just made it).
func (st *dbState) addKey(ctx context.Context, key string) error {
	metas, err := st.db.Metas(ctx, []string{key})
	if err != nil {
		return err
	}
	if len(metas) != 1 || metas[0].Type == TypeNone {
		return nil
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	st.tree.Add(keytree.Leaf{Key: key, Type: metas[0].Type, TTLMillis: metas[0].TTLMillis})
	return nil
}

// snapshot is what the node lists need, taken under the lock.
type snapshot struct {
	entries []keytree.Entry
	more    bool
	loaded  int
	size    int64
	filter  string
}

// level lists the rows below a folder path.
func (st *dbState) level(path string) snapshot {
	st.mu.Lock()
	defer st.mu.Unlock()
	return snapshot{entries: st.tree.Children(path), more: st.started && !st.cur.Done(), loaded: st.tree.Len(), size: st.size, filter: st.filterText()}
}

// ---- nodes ----

// FormatTTL writes a time to live from PTTL milliseconds: "no expiry" for -1, "gone" for -2, "59s", "3m 20s", "2h 5m", "4d 3h".
func FormatTTL(ms int64) string {
	switch {
	case ms == -1:
		return "no expiry"
	case ms < 0:
		return "gone"
	case ms < 1000:
		return strconv.FormatInt(ms, 10) + " ms"
	}
	d := time.Duration(ms) * time.Millisecond
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
	}
	return fmt.Sprintf("%dd %dh", int(d.Hours())/24, int(d.Hours())%24)
}

// FormatBytes writes a size in bytes as B, KB, MB or GB.
func FormatBytes(n int64) string {
	const k = 1024.0
	f := float64(n)
	switch {
	case n < 0:
		return "?"
	case f < k:
		return fmt.Sprintf("%d B", n)
	case f < k*k:
		return fmt.Sprintf("%.1f KB", f/k)
	case f < k*k*k:
		return fmt.Sprintf("%.1f MB", f/(k*k))
	}
	return fmt.Sprintf("%.1f GB", f/(k*k*k))
}

// Commas writes a number with thousands separators.
func Commas(n int64) string {
	s := strconv.FormatInt(n, 10)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	if neg {
		s = "-" + s
	}
	return s
}

// plural writes a count and its noun.
func plural(n int64, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return Commas(n) + " " + many
}
