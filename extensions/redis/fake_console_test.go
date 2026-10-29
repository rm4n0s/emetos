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

// The console part of the in-memory server: Do, Pub/Sub and Monitor.

// fakeFlags are the flags COMMAND INFO gives the commands the fake knows: "write" for those that change data.
var fakeFlags = map[string][]string{
	"set": {"write", "denyoom"}, "del": {"write"}, "flushall": {"write"}, "flushdb": {"write"}, "hset": {"write"}, "lpush": {"write"},
	"sadd": {"write"}, "zadd": {"write"}, "expire": {"write"}, "rename": {"write"}, "incr": {"write"}, "append": {"write"},
	"mymodule.write": {"write"},
	"get":            {"readonly", "fast"}, "hgetall": {"readonly"}, "keys": {"readonly"}, "lrange": {"readonly"}, "smembers": {"readonly"},
	"ping": {"fast"}, "info": {"loading"}, "echo": {"fast"}, "dbsize": {"readonly", "fast"}, "ttl": {"readonly", "fast"}, "type": {"readonly", "fast"},
	"config|get": {"admin", "loading"}, "config|set": {"admin", "loading"}, "publish": {"pubsub", "fast"}, "blpop": {"write", "blocking"},
	"command": {"loading"}, "slowlog|get": {"admin"}, "client|list": {"admin"}, "memory|stats": {"readonly"},
}

// fakeDocs are the commands COMMAND DOCS gives, with their summaries.
var fakeDocs = map[string]string{
	"GET": "Returns the string value of a key.", "SET": "Sets the string value of a key, ignoring its type.", "GETDEL": "Returns the string value of a key after deleting the key.",
	"GETEX": "Returns the string value of a key after setting its expiration time.", "HGETALL": "Returns all fields and values of a hash.",
	"DEL": "Deletes one or more keys.", "PING": "Returns the server's liveliness response.",
}

// Do implements DB: a small interpreter of the commands the console tests use.
func (d *fakeDB) Do(ctx context.Context, args []string) (Reply, error) {
	if len(args) == 0 {
		return Reply{}, errors.New("FakeEmpty", "no command")
	}
	if err := d.begin(strings.Join(args, " ")); err != nil {
		return Reply{}, err
	}
	s := d.s
	name := strings.ToUpper(args[0])
	s.mu.Lock()
	refused, isRefused := s.refuse[name]
	s.mu.Unlock()
	if isRefused {
		return Reply{Kind: ReplyError, Str: refused}, nil
	}
	str := func(v string) Reply { return Reply{Kind: ReplyString, Str: v} }
	num := func(n int64) Reply { return Reply{Kind: ReplyInt, Int: n} }
	ok := Reply{Kind: ReplyStatus, Str: "OK"}
	wrong := func() Reply {
		return Reply{Kind: ReplyError, Str: "ERR wrong number of arguments for '" + strings.ToLower(name) + "' command"}
	}
	switch name {
	case "PING":
		if len(args) > 1 {
			return str(args[1]), nil
		}
		return Reply{Kind: ReplyStatus, Str: "PONG"}, nil
	case "ECHO":
		if len(args) != 2 {
			return wrong(), nil
		}
		return str(args[1]), nil
	case "GET":
		if len(args) != 2 {
			return wrong(), nil
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		k := d.keys[args[1]]
		switch {
		case k == nil:
			return Reply{Kind: ReplyNil}, nil
		case k.typ != TypeString:
			return Reply{Kind: ReplyError, Str: "WRONGTYPE Operation against a key holding the wrong kind of value"}, nil
		}
		return str(k.str), nil
	case "SET":
		if len(args) < 3 {
			return wrong(), nil
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		d.keys[args[1]] = &fakeKey{typ: TypeString, str: args[2], ttl: -1}
		return ok, nil
	case "DEL":
		if len(args) < 2 {
			return wrong(), nil
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		var n int64
		for _, k := range args[1:] {
			if d.keys[k] != nil {
				delete(d.keys, k)
				n++
			}
		}
		return num(n), nil
	case "DBSIZE":
		s.mu.Lock()
		defer s.mu.Unlock()
		return num(int64(len(d.keys))), nil
	case "KEYS":
		if len(args) != 2 {
			return wrong(), nil
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		var keys []string
		for k := range d.keys {
			if globMatch(args[1], k) {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		items := make([]Reply, len(keys))
		for i, k := range keys {
			items[i] = str(k)
		}
		if len(items) == 0 {
			return Reply{Kind: ReplyArray}, nil
		}
		return Reply{Kind: ReplyArray, Items: items}, nil
	case "FLUSHDB":
		s.mu.Lock()
		defer s.mu.Unlock()
		d.keys = map[string]*fakeKey{}
		return ok, nil
	case "FLUSHALL":
		s.mu.Lock()
		defer s.mu.Unlock()
		for _, db := range s.dbs {
			db.keys = map[string]*fakeKey{}
		}
		return ok, nil
	case "HGETALL":
		if len(args) != 2 {
			return wrong(), nil
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		k := d.keys[args[1]]
		if k == nil {
			return Reply{Kind: ReplyArray}, nil
		}
		if s.resp3 {
			r := Reply{Kind: ReplyMap}
			for _, p := range k.hash {
				r.Pairs = append(r.Pairs, ReplyPair{Key: str(p.Field), Value: str(p.Value)})
			}
			return r, nil
		}
		r := Reply{Kind: ReplyArray}
		for _, p := range k.hash {
			r.Items = append(r.Items, str(p.Field), str(p.Value))
		}
		return r, nil
	case "SMEMBERS":
		s.mu.Lock()
		defer s.mu.Unlock()
		k := d.keys[args[1]]
		kind := ReplyArray
		if s.resp3 {
			kind = ReplySet
		}
		r := Reply{Kind: kind}
		if k != nil {
			for _, m := range k.set {
				r.Items = append(r.Items, str(m))
			}
		}
		return r, nil
	case "LRANGE":
		s.mu.Lock()
		defer s.mu.Unlock()
		r := Reply{Kind: ReplyArray}
		if k := d.keys[args[1]]; k != nil {
			for _, m := range k.list {
				r.Items = append(r.Items, str(m))
			}
		}
		return r, nil
	case "SELECT":
		return ok, nil
	case "CONFIG":
		if len(args) > 1 && strings.EqualFold(args[1], "SET") {
			return ok, nil
		}
		return Reply{Kind: ReplyArray, Items: []Reply{str("maxmemory"), str("0")}}, nil
	case "EVAL":
		return Reply{Kind: ReplyNil}, nil
	case "BLPOP":
		<-ctx.Done()
		return Reply{}, errors.NewErr("FakeCancelled", ctx.Err())
	case "PUBLISH":
		if len(args) != 3 {
			return wrong(), nil
		}
		return num(int64(s.publish(args[1], args[2]))), nil
	case "INFO":
		return str("# Server\r\nredis_version:8.2.2\r\nredis_mode:standalone\r\n\r\n# Clients\r\nconnected_clients:3\r\n\r\n# Keyspace\r\ndb0:keys=9,expires=1,avg_ttl=0\r\n"), nil
	case "SLOWLOG":
		s.mu.Lock()
		defer s.mu.Unlock()
		return Reply{Kind: ReplyArray, Items: append([]Reply(nil), s.slow...)}, nil
	case "CLIENT":
		if len(args) > 1 && strings.EqualFold(args[1], "LIST") {
			return str("id=3 addr=127.0.0.1:50000 laddr=127.0.0.1:6379 fd=8 name=emetos age=12 idle=0 flags=N db=0 sub=0 psub=0 multi=-1 cmd=client|list user=default\n" +
				"id=4 addr=127.0.0.1:50002 laddr=127.0.0.1:6379 fd=9 name= age=900 idle=45 flags=N db=3 sub=1 psub=0 multi=-1 cmd=subscribe user=default\n"), nil
		}
		return ok, nil
	case "MEMORY":
		if len(args) > 1 && strings.EqualFold(args[1], "STATS") {
			if s.resp3 {
				return Reply{Kind: ReplyMap, Pairs: []ReplyPair{
					{Key: str("peak.allocated"), Value: num(2097152)}, {Key: str("total.allocated"), Value: num(1048576)},
					{Key: str("db.0"), Value: Reply{Kind: ReplyMap, Pairs: []ReplyPair{{Key: str("overhead.hashtable.main"), Value: num(72)}}}},
					{Key: str("fragmentation"), Value: Reply{Kind: ReplyDouble, Float: 1.25}},
				}}, nil
			}
			return Reply{Kind: ReplyArray, Items: []Reply{str("peak.allocated"), num(2097152), str("total.allocated"), num(1048576),
				str("db.0"), {Kind: ReplyArray, Items: []Reply{str("overhead.hashtable.main"), num(72)}}, str("fragmentation"), str("1.25")}}, nil
		}
	case "COMMAND":
		return d.doCommand(args), nil
	case "HELLO":
		if s.resp3 {
			return Reply{Kind: ReplyMap, Pairs: []ReplyPair{{Key: str("server"), Value: str("redis")}, {Key: str("proto"), Value: num(3)}}}, nil
		}
		return Reply{Kind: ReplyError, Str: "ERR unknown command 'HELLO'"}, nil
	}
	return Reply{Kind: ReplyError, Str: "ERR unknown command '" + args[0] + "'"}, nil
}

// doCommand answers COMMAND, COMMAND INFO and COMMAND DOCS.
func (d *fakeDB) doCommand(args []string) Reply {
	s := d.s
	str := func(v string) Reply { return Reply{Kind: ReplyString, Str: v} }
	if s.noCommandInfo {
		return Reply{Kind: ReplyError, Str: "ERR unknown command 'COMMAND'"}
	}
	if len(args) == 1 {
		var out []Reply
		for _, n := range sortedKeys(fakeDocs) {
			out = append(out, Reply{Kind: ReplyArray, Items: []Reply{str(strings.ToLower(n)), {Kind: ReplyInt, Int: -1}}})
		}
		return Reply{Kind: ReplyArray, Items: out}
	}
	switch strings.ToUpper(args[1]) {
	case "INFO":
		var out []Reply
		for _, n := range args[2:] {
			flags, known := fakeFlags[strings.ToLower(n)]
			if !known {
				out = append(out, Reply{Kind: ReplyNil})
				continue
			}
			fl := make([]Reply, len(flags))
			for i, f := range flags {
				fl[i] = Reply{Kind: ReplyStatus, Str: f}
			}
			out = append(out, Reply{Kind: ReplyArray, Items: []Reply{str(n), {Kind: ReplyInt, Int: -1}, {Kind: ReplyArray, Items: fl}}})
		}
		return Reply{Kind: ReplyArray, Items: out}
	case "DOCS":
		names := sortedKeys(fakeDocs)
		if s.resp3 {
			r := Reply{Kind: ReplyMap}
			for _, n := range names {
				r.Pairs = append(r.Pairs, ReplyPair{Key: str(strings.ToLower(n)), Value: Reply{Kind: ReplyMap, Pairs: []ReplyPair{
					{Key: str("summary"), Value: str(fakeDocs[n])}, {Key: str("since"), Value: str("1.0.0")}}}})
			}
			return r
		}
		r := Reply{Kind: ReplyArray}
		for _, n := range names {
			r.Items = append(r.Items, str(strings.ToLower(n)), Reply{Kind: ReplyArray, Items: []Reply{str("summary"), str(fakeDocs[n]), str("since"), str("1.0.0")}})
		}
		return r
	}
	return Reply{Kind: ReplyError, Str: "ERR unknown subcommand"}
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ---- Pub/Sub ----

// fakeSub is a Pub/Sub connection of the fake server.
type fakeSub struct {
	s   *fakeServer
	ch  chan PubSubMessage
	mu  sync.Mutex
	set map[string]bool // "c:name" for a channel, "p:name" for a pattern
	end bool
	err error
}

var _ Subscription = (*fakeSub)(nil)

// PubSub implements Server.
func (s *fakeServer) PubSub(context.Context) (Subscription, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, errors.New("FakeClosed", "the fake server is closed")
	}
	sub := &fakeSub{s: s, ch: make(chan PubSubMessage, 256), set: map[string]bool{}}
	s.subs = append(s.subs, sub)
	return sub, nil
}

// clock is the time the fake stamps a message with.
func (s *fakeServer) clock() time.Time {
	s.mu.Lock()
	now := s.now
	s.mu.Unlock()
	if now != nil {
		return now()
	}
	return time.Now()
}

// publish delivers a message to the subscriptions that match and returns how many received it.
func (s *fakeServer) publish(channel, payload string) int {
	s.mu.Lock()
	subs := append([]*fakeSub(nil), s.subs...)
	s.mu.Unlock()
	n := 0
	for _, sub := range subs {
		sub.mu.Lock()
		if sub.end {
			sub.mu.Unlock()
			continue
		}
		var patterns []string
		hit := sub.set["c:"+channel]
		for k := range sub.set {
			if p, ok := strings.CutPrefix(k, "p:"); ok && globMatch(p, channel) {
				patterns = append(patterns, p)
			}
		}
		sort.Strings(patterns)
		if hit {
			n++
			sub.deliver(PubSubMessage{Channel: channel, Payload: payload})
		}
		for _, p := range patterns {
			n++
			sub.deliver(PubSubMessage{Channel: channel, Pattern: p, Payload: payload})
		}
		sub.mu.Unlock()
	}
	return n
}

// deliver sends a message without blocking; the caller holds sub.mu.
func (f *fakeSub) deliver(m PubSubMessage) {
	m.Time = f.s.clock()
	select {
	case f.ch <- m:
	case <-time.After(2 * time.Second):
	}
}

func (f *fakeSub) Messages() <-chan PubSubMessage { return f.ch }

func (f *fakeSub) add(prefix string, names []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.end {
		return errors.New("FakeClosed", "the subscription is closed")
	}
	for _, n := range names {
		f.set[prefix+n] = true
	}
	return nil
}

func (f *fakeSub) remove(prefix string, names []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, n := range names {
		delete(f.set, prefix+n)
	}
	return nil
}

func (f *fakeSub) Subscribe(_ context.Context, c ...string) error    { return f.add("c:", c) }
func (f *fakeSub) PSubscribe(_ context.Context, p ...string) error   { return f.add("p:", p) }
func (f *fakeSub) Unsubscribe(_ context.Context, c ...string) error  { return f.remove("c:", c) }
func (f *fakeSub) PUnsubscribe(_ context.Context, p ...string) error { return f.remove("p:", p) }

func (f *fakeSub) Err() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.err
}

// fail ends the subscription as a connection that broke does.
func (f *fakeSub) fail(err error) {
	f.mu.Lock()
	if !f.end {
		f.end, f.err = true, err
		close(f.ch)
	}
	f.mu.Unlock()
}

func (f *fakeSub) Close() {
	f.mu.Lock()
	if !f.end {
		f.end = true
		close(f.ch)
	}
	f.mu.Unlock()
}

// closed tells whether the subscription ended.
func (f *fakeSub) closed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.end
}

// ---- Monitor ----

// fakeMon is a MONITOR connection of the fake server.
type fakeMon struct {
	ch  chan string
	mu  sync.Mutex
	end bool
	err error
}

var _ Monitor = (*fakeMon)(nil)

// Monitor implements Server.
func (s *fakeServer) Monitor(context.Context) (Monitor, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, errors.New("FakeClosed", "the fake server is closed")
	}
	if s.mode != ModeStandalone {
		return nil, errors.New("RedisMonitorUnsupported", "MONITOR is available on a standalone server only", "mode", s.mode)
	}
	m := &fakeMon{ch: make(chan string, 256)}
	s.mons = append(s.mons, m)
	return m, nil
}

// push sends a line to the monitors.
func (s *fakeServer) pushMonitor(line string) {
	s.mu.Lock()
	mons := append([]*fakeMon(nil), s.mons...)
	s.mu.Unlock()
	for _, m := range mons {
		m.mu.Lock()
		if !m.end {
			select {
			case m.ch <- line:
			case <-time.After(2 * time.Second):
			}
		}
		m.mu.Unlock()
	}
}

func (m *fakeMon) Lines() <-chan string { return m.ch }

func (m *fakeMon) Err() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.err
}

func (m *fakeMon) Close() {
	m.mu.Lock()
	if !m.end {
		m.end = true
		close(m.ch)
	}
	m.mu.Unlock()
}

func (m *fakeMon) closed() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.end
}

// monitorLine writes a line as the server does.
func monitorLine(secs float64, db int, client, command string) string {
	return strconv.FormatFloat(secs, 'f', 6, 64) + " [" + strconv.Itoa(db) + " " + client + "] " + command
}
