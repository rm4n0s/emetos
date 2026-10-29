package redis

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"slices"
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
	// resp3 makes the server answer HELLO 3 and speak RESP3 on the connections that asked for it.
	resp3 bool

	// the connections in Pub/Sub or Monitor mode
	subs []*respConn
	mons []*respConn
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
	conn := &respConn{db: 0, authed: s.password == "", conn: c}
	defer s.forget(conn)
	for {
		args, err := readCommand(r)
		if err != nil {
			return
		}
		s.mu.Lock()
		s.cmds = append(s.cmds, strings.Join(args, " "))
		s.mu.Unlock()
		if s.special(conn, r, args) {
			continue
		}
		if len(args) > 0 && strings.EqualFold(args[0], "TEST.DROP") {
			return // the connection breaks
		}
		reply := s.handle(conn, args)
		if err := conn.write(reply); err != nil {
			return
		}
		if conn.dead {
			return
		}
		s.broadcastMonitor(conn, args)
	}
}

type respConn struct {
	db     int
	authed bool
	conn   net.Conn
	resp3  bool
	dead   bool

	wmu sync.Mutex
	// channels and patterns this connection listens to; mon says it is in MONITOR mode
	channels, patterns map[string]bool
	mon                bool
}

// write sends a reply; the writes of the goroutines that publish and of the loop of the connection do not interleave.
func (c *respConn) write(reply string) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	_, err := io.WriteString(c.conn, reply)
	return err
}

// forget takes a connection out of the Pub/Sub and Monitor lists.
func (s *respServer) forget(c *respConn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, o := range s.subs {
		if o == c {
			s.subs = append(s.subs[:i], s.subs[i+1:]...)
			break
		}
	}
	for i, o := range s.mons {
		if o == c {
			s.mons = append(s.mons[:i], s.mons[i+1:]...)
			break
		}
	}
}

// special handles the commands that need the connection itself: BLPOP (it waits until the client goes away), SUBSCRIBE and the like, and
// MONITOR. It reports whether it answered.
func (s *respServer) special(c *respConn, r *bufio.Reader, args []string) bool {
	name := strings.ToUpper(args[0])
	if !c.authed && name != "AUTH" && name != "HELLO" {
		return false
	}
	switch name {
	case "BLPOP":
		_, _ = r.Peek(1) // returns when the client closes the connection: nothing else is sent
		return true
	case "SUBSCRIBE", "PSUBSCRIBE":
		s.mu.Lock()
		if c.channels == nil {
			c.channels, c.patterns = map[string]bool{}, map[string]bool{}
			s.subs = append(s.subs, c)
		}
		s.mu.Unlock()
		for _, n := range args[1:] {
			s.mu.Lock()
			if name == "SUBSCRIBE" {
				c.channels[n] = true
			} else {
				c.patterns[n] = true
			}
			count := len(c.channels) + len(c.patterns)
			s.mu.Unlock()
			if err := c.write(rArray(rBulk(strings.ToLower(name)), rBulk(n), rInt(int64(count)))); err != nil {
				return true
			}
		}
		return true
	case "UNSUBSCRIBE", "PUNSUBSCRIBE":
		s.mu.Lock()
		for _, n := range args[1:] {
			if name == "UNSUBSCRIBE" {
				delete(c.channels, n)
			} else {
				delete(c.patterns, n)
			}
		}
		count := len(c.channels) + len(c.patterns)
		s.mu.Unlock()
		for _, n := range args[1:] {
			if err := c.write(rArray(rBulk(strings.ToLower(name)), rBulk(n), rInt(int64(count)))); err != nil {
				return true
			}
		}
		return true
	case "MONITOR":
		s.mu.Lock()
		s.mons = append(s.mons, c)
		c.mon = true
		s.mu.Unlock()
		_ = c.write(rStatus("OK")) // a failed write ends the loop at the next read
		return true
	case "PUBLISH":
		if len(args) == 3 {
			n := s.publish(args[1], args[2])
			_ = c.write(rInt(int64(n)))
			s.broadcastMonitor(c, args)
			return true
		}
	}
	return false
}

// publish delivers a message to the connections that listen to the channel or a pattern that matches, and returns how many received.
func (s *respServer) publish(channel, payload string) int {
	s.mu.Lock()
	subs := append([]*respConn(nil), s.subs...)
	s.mu.Unlock()
	n := 0
	for _, c := range subs {
		s.mu.Lock()
		hit := c.channels[channel]
		var pats []string
		for p := range c.patterns {
			if globMatch(p, channel) {
				pats = append(pats, p)
			}
		}
		s.mu.Unlock()
		if hit {
			n++
			_ = c.write(rArray(rBulk("message"), rBulk(channel), rBulk(payload))) // a client that went away is dropped by its loop
		}
		for _, p := range pats {
			n++
			_ = c.write(rArray(rBulk("pmessage"), rBulk(p), rBulk(channel), rBulk(payload)))
		}
	}
	return n
}

// broadcastMonitor writes a command to the connections in MONITOR mode, as the server does for the commands of the other clients.
func (s *respServer) broadcastMonitor(from *respConn, args []string) {
	s.mu.Lock()
	mons := append([]*respConn(nil), s.mons...)
	s.mu.Unlock()
	if len(mons) == 0 || from.mon || strings.EqualFold(args[0], "AUTH") || strings.EqualFold(args[0], "HELLO") {
		return
	}
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = strconv.Quote(a)
	}
	line := fmt.Sprintf("%.6f [%d %s] %s", 1700000000.25, from.db, from.conn.RemoteAddr(), strings.Join(quoted, " "))
	for _, c := range mons {
		_ = c.write(rStatus(line)) // a client that went away is dropped by its loop
	}
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
		if s.resp3 && len(args) > 1 && args[1] == "3" {
			if s.password != "" {
				if i := slices.Index(args, "AUTH"); i < 0 || args[len(args)-1] != s.password {
					return rErr("WRONGPASS invalid username-password pair or user is disabled.")
				}
				c.authed = true
			}
			c.resp3 = true
			return "%2\r\n" + rBulk("server") + rBulk("redis") + rBulk("proto") + rInt(3)
		}
		if s.resp3 && len(args) == 1 {
			return "%2\r\n" + rBulk("server") + rBulk("redis") + rBulk("proto") + rInt(3)
		}
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
	return s.handleDo(c, fdb, args)
}

// handleDo answers the commands of the console with the reply the fake gives, written as RESP2 or RESP3.
func (s *respServer) handleDo(c *respConn, fdb *fakeDB, args []string) string {
	if strings.EqualFold(args[0], "TEST.TYPES") {
		return testTypesReply(c.resp3)
	}
	rep, err := fdb.Do(context.Background(), args)
	if err != nil {
		return rErr("ERR " + err.Error())
	}
	return encodeReply(rep, c.resp3)
}

// encodeReply writes a reply on the wire. In RESP2 the types RESP3 added are written as the server writes them to a RESP2 client.
func encodeReply(r Reply, resp3 bool) string {
	switch r.Kind {
	case ReplyNil:
		if resp3 {
			return "_\r\n"
		}
		return "$-1\r\n"
	case ReplyStatus:
		return rStatus(r.Str)
	case ReplyError:
		return rErr(r.Str)
	case ReplyInt:
		return rInt(r.Int)
	case ReplyString:
		return rBulk(r.Str)
	case ReplyDouble:
		if resp3 {
			return "," + strconv.FormatFloat(r.Float, 'g', -1, 64) + "\r\n"
		}
		return rBulk(strconv.FormatFloat(r.Float, 'g', -1, 64))
	case ReplyBool:
		if resp3 {
			return map[bool]string{true: "#t\r\n", false: "#f\r\n"}[r.Bool]
		}
		return rInt(map[bool]int64{true: 1, false: 0}[r.Bool])
	case ReplyBigNumber:
		if resp3 {
			return "(" + r.Str + "\r\n"
		}
		return rBulk(r.Str)
	case ReplyArray, ReplySet:
		parts := make([]string, len(r.Items))
		for i, it := range r.Items {
			parts[i] = encodeReply(it, resp3)
		}
		if r.Kind == ReplySet && resp3 {
			return "~" + strconv.Itoa(len(parts)) + "\r\n" + strings.Join(parts, "")
		}
		return rArray(parts...)
	case ReplyMap:
		var parts []string
		for _, p := range r.Pairs {
			parts = append(parts, encodeReply(p.Key, resp3), encodeReply(p.Value, resp3))
		}
		if resp3 {
			return "%" + strconv.Itoa(len(r.Pairs)) + "\r\n" + strings.Join(parts, "")
		}
		return rArray(parts...)
	}
	return rErr("ERR the test server cannot write this reply")
}

// testTypesReply is an array with one value of each type RESP3 has (what RESP2 has, in RESP2).
func testTypesReply(resp3 bool) string {
	if !resp3 {
		return rArray("$-1\r\n", rInt(7), rBulk("text"), rBulk("1.5"), rInt(1), rBulk("123456789012345678901234567890"), rArray(rBulk("k"), rBulk("v")), rArray(rBulk("m")),
			rErr("WRONGTYPE inside"))
	}
	return "*10\r\n_\r\n" + rInt(7) + rBulk("text") + ",1.5\r\n#t\r\n(123456789012345678901234567890\r\n" + "%1\r\n" + rBulk("k") + rBulk("v") +
		"~1\r\n" + rBulk("m") + "=15\r\ntxt:hello world\r\n" + rErr("WRONGTYPE inside")
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
