package redis

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
)

// respServer speaks RESP2 on a local port and answers the commands the Go client sends from a fakeServer. It lets the tests of the
// real client check the commands it sends (their names and the order of their arguments) without a Redis. It listens on 127.0.0.1
// only and is closed with the test.
type respServer struct {
	t        testing.TB
	ln       net.Listener
	store    *fakeServer
	password string

	mu   sync.Mutex
	cmds []string
	wg   sync.WaitGroup
	// failAll makes every command except the handshake fail with an error.
	failAll atomic.Bool
	// failing holds the names of commands that fail with an error, whatever their arguments.
	failing map[string]bool
	// zscore writes a score for ZSCAN; FormatScore when nil.
	zscore func(float64) string
}

func newRESPServer(t testing.TB, store *fakeServer, password string) *respServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &respServer{t: t, ln: ln, store: store, password: password}
	s.wg.Add(1)
	go s.accept()
	t.Cleanup(func() {
		_ = ln.Close() // the listener is ours; the accept loop ends on the error
		s.wg.Wait()
	})
	return s
}

func (s *respServer) addr() string { return s.ln.Addr().String() }

// commands returns the commands received, "NAME arg arg".
func (s *respServer) commands() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.cmds...)
}

func (s *respServer) accept() {
	defer s.wg.Done()
	for {
		c, err := s.ln.Accept()
		if err != nil {
			return
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.serve(c)
			if err := c.Close(); err != nil {
				s.t.Logf("closing a connection: %v", err)
			}
		}()
	}
}

func (s *respServer) serve(c net.Conn) {
	r := bufio.NewReader(c)
	conn := &respConn{db: 0, authed: s.password == ""}
	for {
		args, err := readCommand(r)
		if err != nil {
			return
		}
		s.mu.Lock()
		s.cmds = append(s.cmds, strings.Join(args, " "))
		s.mu.Unlock()
		reply := s.handle(conn, args)
		if _, err := io.WriteString(c, reply); err != nil {
			return
		}
	}
}

type respConn struct {
	db     int
	authed bool
}

func readCommand(r *bufio.Reader) ([]string, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(line, "*") {
		return nil, errors.New("TestRESPNotArray", "not an array", "line", line)
	}
	n, err := strconv.Atoi(strings.TrimSpace(line[1:]))
	if err != nil {
		return nil, err
	}
	args := make([]string, 0, n)
	for i := 0; i < n; i++ {
		head, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		size, err := strconv.Atoi(strings.TrimSpace(head[1:]))
		if err != nil {
			return nil, err
		}
		buf := make([]byte, size+2)
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, err
		}
		args = append(args, string(buf[:size]))
	}
	return args, nil
}

func rStatus(s string) string { return "+" + s + "\r\n" }
func rErr(s string) string    { return "-" + s + "\r\n" }
func rInt(n int64) string     { return ":" + strconv.FormatInt(n, 10) + "\r\n" }
func rBulk(s string) string   { return "$" + strconv.Itoa(len(s)) + "\r\n" + s + "\r\n" }
func rArray(parts ...string) string {
	return "*" + strconv.Itoa(len(parts)) + "\r\n" + strings.Join(parts, "")
}
func rBulks(items ...string) string {
	parts := make([]string, len(items))
	for i, it := range items {
		parts[i] = rBulk(it)
	}
	return rArray(parts...)
}

func (s *respServer) handle(c *respConn, args []string) string {
	name := strings.ToUpper(args[0])
	switch name {
	case "HELLO":
		return rErr("ERR unknown command 'HELLO'")
	case "AUTH":
		pass := args[len(args)-1]
		if s.password == "" || pass != s.password {
			return rErr("WRONGPASS invalid username-password pair or user is disabled.")
		}
		c.authed = true
		return rStatus("OK")
	}
	if !c.authed {
		return rErr("NOAUTH Authentication required.")
	}
	s.mu.Lock()
	failing := s.failing[name]
	s.mu.Unlock()
	if failing {
		return rErr("ERR boom")
	}
	if s.failAll.Load() && name != "SELECT" && name != "CLIENT" && name != "PING" {
		return rErr("ERR boom")
	}
	ctx := context.Background()
	db, err := s.store.DB(c.db)
	if err != nil {
		return rErr("ERR " + err.Error())
	}
	fdb := db.(*fakeDB)
	fail := func(err error) string { return rErr("ERR " + err.Error()) }
	atoi := func(i int) int64 { n, _ := strconv.ParseInt(args[i], 10, 64); return n }
	switch name {
	case "PING":
		return rStatus("PONG")
	case "SELECT":
		c.db = int(atoi(1))
		return rStatus("OK")
	case "CLIENT":
		return rStatus("OK")
	case "INFO":
		infos, _ := s.store.Databases(ctx)
		var b strings.Builder
		b.WriteString("# Keyspace\r\n")
		for _, in := range infos {
			fmt.Fprintf(&b, "db%d:keys=%d,expires=%d,avg_ttl=0\r\n", in.Index, in.Keys, in.Expires)
		}
		return rBulk(b.String())
	case "DBSIZE":
		n, err := fdb.DBSize(ctx)
		if err != nil {
			return fail(err)
		}
		return rInt(n)
	case "SCAN":
		match, count := "", int64(10)
		for i := 2; i+1 < len(args); i += 2 {
			switch strings.ToUpper(args[i]) {
			case "MATCH":
				match = args[i+1]
			case "COUNT":
				count = atoi(i + 1)
			}
		}
		keys, next, err := fdb.Scan(ctx, Cursor{Pos: uint64(atoi(1))}, match, count)
		if err != nil {
			return fail(err)
		}
		cur := "0"
		if !next.End {
			cur = strconv.FormatUint(next.Pos, 10)
		}
		return rArray(rBulk(cur), rBulks(keys...))
	case "TYPE":
		metas, _ := fdb.Metas(ctx, []string{args[1]})
		return rStatus(metas[0].Type)
	case "PTTL":
		metas, _ := fdb.Metas(ctx, []string{args[1]})
		return rInt(metas[0].TTLMillis)
	case "MEMORY":
		info, _ := fdb.Info(ctx, args[2])
		return rInt(info.Memory)
	case "OBJECT":
		info, _ := fdb.Info(ctx, args[2])
		return rBulk(info.Encoding)
	case "STRLEN", "HLEN", "LLEN", "SCARD", "ZCARD", "XLEN":
		info, _ := fdb.Info(ctx, args[1])
		return rInt(max(info.Size, 0))
	case "GETRANGE":
		data, _, err := fdb.GetRange(ctx, args[1], atoi(3)+1)
		if err != nil {
			return fail(err)
		}
		return rBulk(data)
	case "SET":
		keep := false
		for _, a := range args[3:] {
			keep = keep || strings.EqualFold(a, "KEEPTTL")
		}
		if !keep {
			return rErr("ERR the test expects SET with KEEPTTL")
		}
		if err := fdb.Set(ctx, args[1], args[2]); err != nil {
			return fail(err)
		}
		return rStatus("OK")
	case "HSCAN":
		fields, next, err := fdb.HScan(ctx, args[1], uint64(atoi(2)), 10)
		if err != nil {
			return fail(err)
		}
		var flat []string
		for _, f := range fields {
			flat = append(flat, f.Field, f.Value)
		}
		return rArray(rBulk(strconv.FormatUint(next, 10)), rBulks(flat...))
	case "HSET":
		added, err := fdb.HSet(ctx, args[1], args[2], args[3])
		if err != nil {
			return fail(err)
		}
		return rInt(map[bool]int64{true: 1, false: 0}[added])
	case "HDEL":
		if err := fdb.HDel(ctx, args[1], args[2:]...); err != nil {
			return fail(err)
		}
		return rInt(int64(len(args) - 2))
	case "LRANGE":
		items, err := fdb.LRange(ctx, args[1], atoi(2), atoi(3))
		if err != nil {
			return fail(err)
		}
		return rBulks(items...)
	case "LPUSH":
		if err := fdb.LPush(ctx, args[1], args[2]); err != nil {
			return fail(err)
		}
		return rInt(1)
	case "RPUSH":
		if err := fdb.RPush(ctx, args[1], args[2]); err != nil {
			return fail(err)
		}
		return rInt(1)
	case "LSET":
		if err := fdb.LSet(ctx, args[1], atoi(2), args[3]); err != nil {
			return fail(err)
		}
		return rStatus("OK")
	case "LREM":
		// the client set a marker at an index: remove the item that holds it
		s.store.mu.Lock()
		k := fdb.keys[args[1]]
		n := int64(0)
		if k != nil {
			for i, v := range k.list {
				if v == args[3] {
					k.list = append(k.list[:i], k.list[i+1:]...)
					n = 1
					break
				}
			}
		}
		s.store.mu.Unlock()
		return rInt(n)
	case "SSCAN":
		members, next, err := fdb.SScan(ctx, args[1], uint64(atoi(2)), 10)
		if err != nil {
			return fail(err)
		}
		return rArray(rBulk(strconv.FormatUint(next, 10)), rBulks(members...))
	case "SADD":
		added, err := fdb.SAdd(ctx, args[1], args[2])
		if err != nil {
			return fail(err)
		}
		return rInt(map[bool]int64{true: 1, false: 0}[added])
	case "SREM":
		if err := fdb.SRem(ctx, args[1], args[2:]...); err != nil {
			return fail(err)
		}
		return rInt(int64(len(args) - 2))
	case "ZSCAN":
		members, next, err := fdb.ZScan(ctx, args[1], uint64(atoi(2)), 10)
		if err != nil {
			return fail(err)
		}
		var flat []string
		for _, m := range members {
			text := FormatScore(m.Score)
			if s.zscore != nil {
				text = s.zscore(m.Score)
			}
			flat = append(flat, m.Member, text)
		}
		return rArray(rBulk(strconv.FormatUint(next, 10)), rBulks(flat...))
	case "ZADD":
		score, _ := strconv.ParseFloat(args[2], 64)
		added, err := fdb.ZAdd(ctx, args[1], args[3], score)
		if err != nil {
			return fail(err)
		}
		return rInt(map[bool]int64{true: 1, false: 0}[added])
	case "ZREM":
		if err := fdb.ZRem(ctx, args[1], args[2:]...); err != nil {
			return fail(err)
		}
		return rInt(int64(len(args) - 2))
	case "XREVRANGE":
		entries, err := fdb.XRevRange(ctx, args[1], args[2], atoi(5))
		if err != nil {
			return fail(err)
		}
		parts := make([]string, len(entries))
		for i, e := range entries {
			var flat []string
			for _, f := range e.Fields {
				flat = append(flat, f.Field, f.Value)
			}
			parts[i] = rArray(rBulk(e.ID), rBulks(flat...))
		}
		return rArray(parts...)
	case "XADD":
		var fields []Pair
		for i := 3; i+1 < len(args); i += 2 {
			fields = append(fields, Pair{args[i], args[i+1]})
		}
		id, err := fdb.XAdd(ctx, args[1], args[2], fields)
		if err != nil {
			return fail(err)
		}
		return rBulk(id)
	case "XDEL":
		if err := fdb.XDel(ctx, args[1], args[2:]...); err != nil {
			return fail(err)
		}
		return rInt(int64(len(args) - 2))
	case "JSON.GET":
		doc, err := fdb.JSONGet(ctx, args[1])
		if err != nil {
			return fail(err)
		}
		return rBulk(doc)
	case "JSON.SET":
		if args[2] != "$" {
			return rErr("ERR the test expects the path $")
		}
		if err := fdb.JSONSet(ctx, args[1], args[3]); err != nil {
			return fail(err)
		}
		return rStatus("OK")
	case "EXISTS":
		ok, _ := fdb.Exists(ctx, args[1])
		return rInt(map[bool]int64{true: 1, false: 0}[ok])
	case "RENAMENX":
		ok, err := fdb.Rename(ctx, args[1], args[2])
		if err != nil {
			return fail(err)
		}
		return rInt(map[bool]int64{true: 1, false: 0}[ok])
	case "PEXPIRE":
		if err := fdb.Expire(ctx, args[1], time.Duration(atoi(2))*time.Millisecond); err != nil {
			return rInt(0)
		}
		return rInt(1)
	case "PERSIST":
		ok, _ := fdb.Persist(ctx, args[1])
		return rInt(map[bool]int64{true: 1, false: 0}[ok])
	case "DEL":
		n, err := fdb.Del(ctx, args[1:]...)
		if err != nil {
			return fail(err)
		}
		return rInt(n)
	}
	return rErr("ERR unknown command '" + args[0] + "'")
}

// failCommand makes a command fail from now on.
func (s *respServer) failCommand(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failing == nil {
		s.failing = map[string]bool{}
	}
	s.failing[strings.ToUpper(name)] = true
}
