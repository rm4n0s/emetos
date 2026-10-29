package redis

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rm4n0s/errors"
)

// fakeServer is an in-memory Redis for the tests: the commands the client interface needs, with the semantics of the real ones that
// matter (SCAN pages and cursors, KEEPTTL, RENAMENX, the order of a hash). It records the commands it was given.
type fakeServer struct {
	mu     sync.Mutex
	mode   string
	dbs    map[int]*fakeDB
	closed bool
	log    []string
	// fail makes the next command with this name fail with the error.
	fail map[string]error
	// batch, when above zero, limits how many keys one SCAN call returns, whatever COUNT says.
	batch int

	// The console (step 21.2); see fake_console_test.go.
	resp3         bool
	noCommandInfo bool
	subs          []*fakeSub
	mons          []*fakeMon
	slow          []Reply
	// refuse makes Do answer an error reply (this text) to the command with this name.
	refuse map[string]string
	// now is the clock of the messages; time.Now when nil.
	now func() time.Time
}

type fakeKey struct {
	typ    string
	str    string
	hash   []Pair
	list   []string
	set    []string
	zset   []ZMember
	stream []StreamEntry // oldest first
	json   string
	ttl    int64 // milliseconds, -1 for none
}

type fakeDB struct {
	s    *fakeServer
	idx  int
	keys map[string]*fakeKey
}

var (
	_ Server = (*fakeServer)(nil)
	_ DB     = (*fakeDB)(nil)
)

func newFakeServer() *fakeServer {
	return &fakeServer{mode: ModeStandalone, dbs: map[int]*fakeDB{}, fail: map[string]error{}}
}

// put adds a key to a database.
func (s *fakeServer) put(index int, key string, k *fakeKey) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if k.ttl == 0 {
		k.ttl = -1
	}
	s.db(index).keys[key] = k
}

// db returns the database, made on first use. The caller holds mu.
func (s *fakeServer) db(index int) *fakeDB {
	d := s.dbs[index]
	if d == nil {
		d = &fakeDB{s: s, idx: index, keys: map[string]*fakeKey{}}
		s.dbs[index] = d
	}
	return d
}

// commands returns the commands received so far.
func (s *fakeServer) commands() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.log...)
}

// seen tells whether a command with the name was received.
func (s *fakeServer) seen(name string) bool {
	for _, c := range s.commands() {
		if c == name || strings.HasPrefix(c, name+" ") {
			return true
		}
	}
	return false
}

// begin records a command and returns the error it was told to fail with. The caller does not hold mu.
func (d *fakeDB) begin(name string, args ...string) error {
	d.s.mu.Lock()
	defer d.s.mu.Unlock()
	d.s.log = append(d.s.log, strings.TrimSpace(name+" "+strings.Join(args, " ")))
	if err := d.s.fail[name]; err != nil {
		delete(d.s.fail, name)
		return err
	}
	if d.s.closed {
		return errors.New("FakeClosed", "the fake server is closed")
	}
	return nil
}

func (s *fakeServer) Mode() string { return s.mode }

func (s *fakeServer) Databases(context.Context) ([]DBInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.log = append(s.log, "INFO keyspace")
	var out []DBInfo
	for i, d := range s.dbs {
		if len(d.keys) == 0 {
			continue
		}
		in := DBInfo{Index: i, Keys: int64(len(d.keys))}
		for _, k := range d.keys {
			if k.ttl >= 0 {
				in.Expires++
			}
		}
		out = append(out, in)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Index < out[j].Index })
	return out, nil
}

func (s *fakeServer) DB(index int) (DB, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, errors.New("FakeClosed", "the fake server is closed")
	}
	return s.db(index), nil
}

func (s *fakeServer) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

func (d *fakeDB) Index() int { return d.idx }

func (d *fakeDB) DBSize(context.Context) (int64, error) {
	if err := d.begin("DBSIZE"); err != nil {
		return 0, err
	}
	d.s.mu.Lock()
	defer d.s.mu.Unlock()
	return int64(len(d.keys)), nil
}

// globMatch is Redis's glob: * ? [abc] [a-c] [^a] and \ to escape.
func globMatch(pattern, s string) bool {
	for len(pattern) > 0 {
		switch pattern[0] {
		case '*':
			for len(pattern) > 1 && pattern[1] == '*' {
				pattern = pattern[1:]
			}
			if len(pattern) == 1 {
				return true
			}
			for i := 0; i <= len(s); i++ {
				if globMatch(pattern[1:], s[i:]) {
					return true
				}
			}
			return false
		case '?':
			if len(s) == 0 {
				return false
			}
			s = s[1:]
			pattern = pattern[1:]
		case '[':
			if len(s) == 0 {
				return false
			}
			end := strings.IndexByte(pattern, ']')
			if end < 0 {
				return false
			}
			set := pattern[1:end]
			neg := strings.HasPrefix(set, "^")
			set = strings.TrimPrefix(set, "^")
			hit := false
			for i := 0; i < len(set); i++ {
				if i+2 < len(set) && set[i+1] == '-' {
					if s[0] >= set[i] && s[0] <= set[i+2] {
						hit = true
					}
					i += 2
				} else if set[i] == s[0] {
					hit = true
				}
			}
			if hit == neg {
				return false
			}
			s = s[1:]
			pattern = pattern[end+1:]
		case '\\':
			if len(pattern) > 1 {
				pattern = pattern[1:]
			}
			fallthrough
		default:
			if len(s) == 0 || s[0] != pattern[0] {
				return false
			}
			s = s[1:]
			pattern = pattern[1:]
		}
	}
	return len(s) == 0
}

func (d *fakeDB) Scan(_ context.Context, cur Cursor, match string, count int64) ([]string, Cursor, error) {
	if err := d.begin("SCAN", strconv.FormatUint(cur.Pos, 10), "MATCH", match, "COUNT", strconv.FormatInt(count, 10)); err != nil {
		return nil, cur, err
	}
	d.s.mu.Lock()
	defer d.s.mu.Unlock()
	all := make([]string, 0, len(d.keys))
	for k := range d.keys {
		all = append(all, k)
	}
	sort.Strings(all)
	n := int(count)
	if d.s.batch > 0 {
		n = d.s.batch
	}
	start := int(cur.Pos)
	end := min(start+n, len(all))
	var out []string
	for _, k := range all[start:end] {
		if match == "" || globMatch(match, k) {
			out = append(out, k)
		}
	}
	if end >= len(all) {
		return out, Cursor{End: true}, nil
	}
	return out, Cursor{Pos: uint64(end)}, nil
}

func (d *fakeDB) Metas(_ context.Context, keys []string) ([]KeyMeta, error) {
	if err := d.begin("PIPELINE TYPE PTTL"); err != nil {
		return nil, err
	}
	d.s.mu.Lock()
	defer d.s.mu.Unlock()
	out := make([]KeyMeta, len(keys))
	for i, k := range keys {
		if v := d.keys[k]; v != nil {
			out[i] = KeyMeta{Type: v.typ, TTLMillis: v.ttl}
		} else {
			out[i] = KeyMeta{Type: TypeNone, TTLMillis: -2}
		}
	}
	return out, nil
}

func (d *fakeDB) size(k *fakeKey) int64 {
	switch k.typ {
	case TypeString:
		return int64(len(k.str))
	case TypeHash:
		return int64(len(k.hash))
	case TypeList:
		return int64(len(k.list))
	case TypeSet:
		return int64(len(k.set))
	case TypeZSet:
		return int64(len(k.zset))
	case TypeStream:
		return int64(len(k.stream))
	}
	return -1
}

func (d *fakeDB) Info(_ context.Context, key string) (KeyInfo, error) {
	if err := d.begin("TYPE", key); err != nil {
		return KeyInfo{}, err
	}
	d.s.mu.Lock()
	defer d.s.mu.Unlock()
	k := d.keys[key]
	if k == nil {
		return KeyInfo{Type: TypeNone, TTLMillis: -2, Memory: -1, Size: -1}, nil
	}
	return KeyInfo{Type: k.typ, TTLMillis: k.ttl, Memory: 56 + d.size(k)*8, Encoding: "listpack", Size: d.size(k)}, nil
}

// get returns the key if it has the type; the caller holds mu.
func (d *fakeDB) get(key, typ string) (*fakeKey, error) {
	k := d.keys[key]
	if k == nil {
		return nil, nil
	}
	if k.typ != typ {
		return nil, errors.New("FakeWrongType", "WRONGTYPE Operation against a key holding the wrong kind of value")
	}
	return k, nil
}

// make returns the key, creating it with the type.
func (d *fakeDB) make(key, typ string) (*fakeKey, error) {
	k, err := d.get(key, typ)
	if err != nil {
		return nil, err
	}
	if k == nil {
		k = &fakeKey{typ: typ, ttl: -1}
		d.keys[key] = k
	}
	return k, nil
}

// drop removes the key when it holds nothing, as Redis does.
func (d *fakeDB) drop(key string) {
	k := d.keys[key]
	if k != nil && d.size(k) == 0 && k.typ != TypeString && k.typ != TypeStream {
		delete(d.keys, key)
	}
}

func (d *fakeDB) GetRange(_ context.Context, key string, max int64) (string, int64, error) {
	if err := d.begin("GETRANGE", key, "0", strconv.FormatInt(max-1, 10)); err != nil {
		return "", 0, err
	}
	d.s.mu.Lock()
	defer d.s.mu.Unlock()
	k, err := d.get(key, TypeString)
	if err != nil || k == nil {
		return "", 0, err
	}
	return k.str[:min(int64(len(k.str)), max)], int64(len(k.str)), nil
}

func (d *fakeDB) Set(_ context.Context, key, value string) error {
	if err := d.begin("SET", key, "KEEPTTL"); err != nil {
		return err
	}
	d.s.mu.Lock()
	defer d.s.mu.Unlock()
	if old := d.keys[key]; old != nil {
		ttl := old.ttl
		d.keys[key] = &fakeKey{typ: TypeString, str: value, ttl: ttl}
		return nil
	}
	d.keys[key] = &fakeKey{typ: TypeString, str: value, ttl: -1}
	return nil
}

// pageOf returns a page of n items from start, and the next cursor (0 at the end).
func pageOf(total, cursor, n int) (from, to int, next uint64) {
	from = min(cursor, total)
	to = min(from+n, total)
	if to < total {
		next = uint64(to)
	}
	return from, to, next
}

func (d *fakeDB) HScan(_ context.Context, key string, cursor uint64, count int64) ([]Pair, uint64, error) {
	if err := d.begin("HSCAN", key, strconv.FormatUint(cursor, 10)); err != nil {
		return nil, 0, err
	}
	d.s.mu.Lock()
	defer d.s.mu.Unlock()
	k, err := d.get(key, TypeHash)
	if err != nil || k == nil {
		return nil, 0, err
	}
	n := int(count)
	if d.s.batch > 0 {
		n = d.s.batch
	}
	from, to, next := pageOf(len(k.hash), int(cursor), n)
	return append([]Pair(nil), k.hash[from:to]...), next, nil
}

func (d *fakeDB) HSet(_ context.Context, key, field, value string) (bool, error) {
	if err := d.begin("HSET", key, field); err != nil {
		return false, err
	}
	d.s.mu.Lock()
	defer d.s.mu.Unlock()
	k, err := d.make(key, TypeHash)
	if err != nil {
		return false, err
	}
	for i := range k.hash {
		if k.hash[i].Field == field {
			k.hash[i].Value = value
			return false, nil
		}
	}
	k.hash = append(k.hash, Pair{field, value})
	return true, nil
}

func (d *fakeDB) HDel(_ context.Context, key string, fields ...string) error {
	if err := d.begin("HDEL", append([]string{key}, fields...)...); err != nil {
		return err
	}
	d.s.mu.Lock()
	defer d.s.mu.Unlock()
	k, err := d.get(key, TypeHash)
	if err != nil || k == nil {
		return err
	}
	kept := k.hash[:0]
	for _, p := range k.hash {
		gone := false
		for _, f := range fields {
			gone = gone || f == p.Field
		}
		if !gone {
			kept = append(kept, p)
		}
	}
	k.hash = kept
	d.drop(key)
	return nil
}

func (d *fakeDB) LRange(_ context.Context, key string, start, stop int64) ([]string, error) {
	if err := d.begin("LRANGE", key, strconv.FormatInt(start, 10), strconv.FormatInt(stop, 10)); err != nil {
		return nil, err
	}
	d.s.mu.Lock()
	defer d.s.mu.Unlock()
	k, err := d.get(key, TypeList)
	if err != nil || k == nil {
		return nil, err
	}
	if stop < 0 {
		stop += int64(len(k.list))
	}
	from := min(int(start), len(k.list))
	to := min(int(stop)+1, len(k.list))
	if to < from {
		return nil, nil
	}
	return append([]string(nil), k.list[from:to]...), nil
}

func (d *fakeDB) LPush(_ context.Context, key, value string) error {
	if err := d.begin("LPUSH", key); err != nil {
		return err
	}
	d.s.mu.Lock()
	defer d.s.mu.Unlock()
	k, err := d.make(key, TypeList)
	if err != nil {
		return err
	}
	k.list = append([]string{value}, k.list...)
	return nil
}

func (d *fakeDB) RPush(_ context.Context, key, value string) error {
	if err := d.begin("RPUSH", key); err != nil {
		return err
	}
	d.s.mu.Lock()
	defer d.s.mu.Unlock()
	k, err := d.make(key, TypeList)
	if err != nil {
		return err
	}
	k.list = append(k.list, value)
	return nil
}

func (d *fakeDB) LSet(_ context.Context, key string, index int64, value string) error {
	if err := d.begin("LSET", key, strconv.FormatInt(index, 10)); err != nil {
		return err
	}
	d.s.mu.Lock()
	defer d.s.mu.Unlock()
	k, err := d.get(key, TypeList)
	if err != nil {
		return err
	}
	if k == nil || index < 0 || int(index) >= len(k.list) {
		return errors.New("FakeIndex", "ERR index out of range")
	}
	k.list[index] = value
	return nil
}

func (d *fakeDB) LRemoveAt(_ context.Context, key string, index int64) error {
	if err := d.begin("LREMOVEAT", key, strconv.FormatInt(index, 10)); err != nil {
		return err
	}
	d.s.mu.Lock()
	defer d.s.mu.Unlock()
	k, err := d.get(key, TypeList)
	if err != nil {
		return err
	}
	if k == nil || index < 0 || int(index) >= len(k.list) {
		return errors.New("FakeIndex", "ERR index out of range")
	}
	k.list = append(k.list[:index], k.list[index+1:]...)
	d.drop(key)
	return nil
}

func (d *fakeDB) SScan(_ context.Context, key string, cursor uint64, count int64) ([]string, uint64, error) {
	if err := d.begin("SSCAN", key, strconv.FormatUint(cursor, 10)); err != nil {
		return nil, 0, err
	}
	d.s.mu.Lock()
	defer d.s.mu.Unlock()
	k, err := d.get(key, TypeSet)
	if err != nil || k == nil {
		return nil, 0, err
	}
	n := int(count)
	if d.s.batch > 0 {
		n = d.s.batch
	}
	from, to, next := pageOf(len(k.set), int(cursor), n)
	return append([]string(nil), k.set[from:to]...), next, nil
}

func (d *fakeDB) SAdd(_ context.Context, key, member string) (bool, error) {
	if err := d.begin("SADD", key, member); err != nil {
		return false, err
	}
	d.s.mu.Lock()
	defer d.s.mu.Unlock()
	k, err := d.make(key, TypeSet)
	if err != nil {
		return false, err
	}
	for _, m := range k.set {
		if m == member {
			return false, nil
		}
	}
	k.set = append(k.set, member)
	return true, nil
}

func (d *fakeDB) SRem(_ context.Context, key string, members ...string) error {
	if err := d.begin("SREM", append([]string{key}, members...)...); err != nil {
		return err
	}
	d.s.mu.Lock()
	defer d.s.mu.Unlock()
	k, err := d.get(key, TypeSet)
	if err != nil || k == nil {
		return err
	}
	kept := k.set[:0]
	for _, m := range k.set {
		gone := false
		for _, x := range members {
			gone = gone || x == m
		}
		if !gone {
			kept = append(kept, m)
		}
	}
	k.set = kept
	d.drop(key)
	return nil
}

func (d *fakeDB) ZScan(_ context.Context, key string, cursor uint64, count int64) ([]ZMember, uint64, error) {
	if err := d.begin("ZSCAN", key, strconv.FormatUint(cursor, 10)); err != nil {
		return nil, 0, err
	}
	d.s.mu.Lock()
	defer d.s.mu.Unlock()
	k, err := d.get(key, TypeZSet)
	if err != nil || k == nil {
		return nil, 0, err
	}
	n := int(count)
	if d.s.batch > 0 {
		n = d.s.batch
	}
	from, to, next := pageOf(len(k.zset), int(cursor), n)
	return append([]ZMember(nil), k.zset[from:to]...), next, nil
}

func (d *fakeDB) ZAdd(_ context.Context, key, member string, score float64) (bool, error) {
	if err := d.begin("ZADD", key, member); err != nil {
		return false, err
	}
	d.s.mu.Lock()
	defer d.s.mu.Unlock()
	k, err := d.make(key, TypeZSet)
	if err != nil {
		return false, err
	}
	for i := range k.zset {
		if k.zset[i].Member == member {
			k.zset[i].Score = score
			return false, nil
		}
	}
	k.zset = append(k.zset, ZMember{member, score})
	sort.SliceStable(k.zset, func(i, j int) bool { return k.zset[i].Score < k.zset[j].Score })
	return true, nil
}

func (d *fakeDB) ZRem(_ context.Context, key string, members ...string) error {
	if err := d.begin("ZREM", append([]string{key}, members...)...); err != nil {
		return err
	}
	d.s.mu.Lock()
	defer d.s.mu.Unlock()
	k, err := d.get(key, TypeZSet)
	if err != nil || k == nil {
		return err
	}
	kept := k.zset[:0]
	for _, m := range k.zset {
		gone := false
		for _, x := range members {
			gone = gone || x == m.Member
		}
		if !gone {
			kept = append(kept, m)
		}
	}
	k.zset = kept
	d.drop(key)
	return nil
}

func (d *fakeDB) XRevRange(_ context.Context, key, max string, count int64) ([]StreamEntry, error) {
	if err := d.begin("XREVRANGE", key, max, strconv.FormatInt(count, 10)); err != nil {
		return nil, err
	}
	d.s.mu.Lock()
	defer d.s.mu.Unlock()
	k, err := d.get(key, TypeStream)
	if err != nil || k == nil {
		return nil, err
	}
	var out []StreamEntry
	for i := len(k.stream) - 1; i >= 0 && int64(len(out)) < count; i-- {
		e := k.stream[i]
		if max != "+" && streamIDLess(max, e.ID) {
			continue
		}
		out = append(out, e)
	}
	return out, nil
}

// streamIDLess tells whether id a is smaller than id b.
func streamIDLess(a, b string) bool {
	am, as, _ := splitStreamID(a)
	bm, bs, _ := splitStreamID(b)
	return am < bm || (am == bm && as < bs)
}

func (d *fakeDB) XAdd(_ context.Context, key, id string, fields []Pair) (string, error) {
	if err := d.begin("XADD", key, id); err != nil {
		return "", err
	}
	d.s.mu.Lock()
	defer d.s.mu.Unlock()
	k, err := d.make(key, TypeStream)
	if err != nil {
		return "", err
	}
	if id == "*" {
		ms := uint64(1700000000000)
		if n := len(k.stream); n > 0 {
			ms, _, _ = splitStreamID(k.stream[n-1].ID)
			ms++
		}
		id = strconv.FormatUint(ms, 10) + "-0"
	}
	if n := len(k.stream); n > 0 && !streamIDLess(k.stream[n-1].ID, id) {
		return "", errors.New("FakeStreamID", "ERR The ID specified in XADD is equal or smaller than the target stream top item")
	}
	k.stream = append(k.stream, StreamEntry{ID: id, Fields: append([]Pair(nil), fields...)})
	return id, nil
}

func (d *fakeDB) XDel(_ context.Context, key string, ids ...string) error {
	if err := d.begin("XDEL", append([]string{key}, ids...)...); err != nil {
		return err
	}
	d.s.mu.Lock()
	defer d.s.mu.Unlock()
	k, err := d.get(key, TypeStream)
	if err != nil || k == nil {
		return err
	}
	kept := k.stream[:0]
	for _, e := range k.stream {
		gone := false
		for _, id := range ids {
			gone = gone || id == e.ID
		}
		if !gone {
			kept = append(kept, e)
		}
	}
	k.stream = kept
	return nil
}

func (d *fakeDB) JSONGet(_ context.Context, key string) (string, error) {
	if err := d.begin("JSON.GET", key); err != nil {
		return "", err
	}
	d.s.mu.Lock()
	defer d.s.mu.Unlock()
	k, err := d.get(key, TypeJSON)
	if err != nil || k == nil {
		return "", err
	}
	return k.json, nil
}

func (d *fakeDB) JSONSet(_ context.Context, key, doc string) error {
	if err := d.begin("JSON.SET", key); err != nil {
		return err
	}
	d.s.mu.Lock()
	defer d.s.mu.Unlock()
	if old := d.keys[key]; old != nil && old.typ != TypeJSON {
		return errors.New("FakeWrongType", "WRONGTYPE Operation against a key holding the wrong kind of value")
	}
	ttl := int64(-1)
	if old := d.keys[key]; old != nil {
		ttl = old.ttl
	}
	d.keys[key] = &fakeKey{typ: TypeJSON, json: doc, ttl: ttl}
	return nil
}

func (d *fakeDB) Exists(_ context.Context, key string) (bool, error) {
	if err := d.begin("EXISTS", key); err != nil {
		return false, err
	}
	d.s.mu.Lock()
	defer d.s.mu.Unlock()
	return d.keys[key] != nil, nil
}

func (d *fakeDB) Rename(_ context.Context, key, newKey string) (bool, error) {
	if err := d.begin("RENAMENX", key, newKey); err != nil {
		return false, err
	}
	d.s.mu.Lock()
	defer d.s.mu.Unlock()
	k := d.keys[key]
	if k == nil {
		return false, errors.New("FakeNoKey", "ERR no such key")
	}
	if d.keys[newKey] != nil {
		return false, nil
	}
	d.keys[newKey] = k
	delete(d.keys, key)
	return true, nil
}

func (d *fakeDB) Expire(_ context.Context, key string, ttl time.Duration) error {
	if err := d.begin("PEXPIRE", key, ttl.String()); err != nil {
		return err
	}
	d.s.mu.Lock()
	defer d.s.mu.Unlock()
	k := d.keys[key]
	if k == nil {
		return errors.New("RedisKeyMissing", "the key does not exist", "key", key)
	}
	k.ttl = ttl.Milliseconds()
	return nil
}

func (d *fakeDB) Persist(_ context.Context, key string) (bool, error) {
	if err := d.begin("PERSIST", key); err != nil {
		return false, err
	}
	d.s.mu.Lock()
	defer d.s.mu.Unlock()
	k := d.keys[key]
	if k == nil || k.ttl < 0 {
		return false, nil
	}
	k.ttl = -1
	return true, nil
}

func (d *fakeDB) Del(_ context.Context, keys ...string) (int64, error) {
	if err := d.begin("DEL", keys...); err != nil {
		return 0, err
	}
	d.s.mu.Lock()
	defer d.s.mu.Unlock()
	var n int64
	for _, k := range keys {
		if d.keys[k] != nil {
			delete(d.keys, k)
			n++
		}
	}
	return n, nil
}

// fakeDialer opens fake servers by address and records the Specs it was given.
type fakeDialer struct {
	mu      sync.Mutex
	servers map[string]*fakeServer
	err     error
	specs   []Spec
}

var _ Dialer = (*fakeDialer)(nil)

func (f *fakeDialer) Dial(_ context.Context, spec Spec) (Server, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.specs = append(f.specs, spec)
	if f.err != nil {
		return nil, f.err
	}
	srv := f.servers[spec.Addrs[0]]
	if srv == nil {
		return nil, errors.New("FakeRefused", "connection refused", "addr", spec.Addrs[0])
	}
	srv.mu.Lock()
	srv.closed = false
	srv.mode = spec.Mode
	srv.mu.Unlock()
	return srv, nil
}

// fixtureServer is a server with a key of every type in database 0 and a few keys in database 3.
func fixtureServer() *fakeServer {
	s := newFakeServer()
	s.put(0, "greeting", &fakeKey{typ: TypeString, str: "hello world"})
	s.put(0, "config:app", &fakeKey{typ: TypeString, str: `{"debug":true,"level":3}`, ttl: 90_000})
	s.put(0, "user:42:profile", &fakeKey{typ: TypeHash, hash: []Pair{{"name", "Ada"}, {"mail", "ada@example.com"}, {"role", "admin"}}})
	s.put(0, "user:42:tags", &fakeKey{typ: TypeSet, set: []string{"go", "redis", "ui"}})
	s.put(0, "user:7:profile", &fakeKey{typ: TypeHash, hash: []Pair{{"name", "Bob"}}})
	s.put(0, "queue:jobs", &fakeKey{typ: TypeList, list: []string{"build", "test", "deploy"}})
	s.put(0, "scores", &fakeKey{typ: TypeZSet, zset: []ZMember{{"carol", 7.5}, {"alice", 9.25}, {"bob", 12}}})
	s.put(0, "events", &fakeKey{typ: TypeStream, stream: []StreamEntry{
		{"1700000000000-0", []Pair{{"type", "login"}, {"user", "ada"}}},
		{"1700000000001-0", []Pair{{"type", "logout"}, {"user", "ada"}}},
	}})
	s.put(0, "doc:1", &fakeKey{typ: TypeJSON, json: `{"title":"Hello","tags":["a","b"]}`})
	s.put(3, "session:abc", &fakeKey{typ: TypeString, str: "x", ttl: 3_600_000})
	s.put(3, "session:def", &fakeKey{typ: TypeString, str: "y"})
	return s
}
