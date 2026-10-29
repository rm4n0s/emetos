package mongodb

import (
	"context"
	"encoding/binary"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/rm4n0s/emetos/extensions/internal/dbconn"
)

// wireServer is a MongoDB server in a few lines, for the tests of the real Backend: it speaks the wire protocol on 127.0.0.1, answers
// the handshake and the commands the explorer sends from tables, and records what it was asked. It has no authentication.
type wireServer struct {
	t  *testing.T
	ln net.Listener

	mu       sync.Mutex
	commands []bson.D // every command received, in order
	apps     []string // the application names of the handshakes

	// The tables the commands are answered from.
	databases   []bson.D            // the documents of listDatabases
	collections map[string][]bson.D // by database: the documents of listCollections
	indexes     map[string][]bson.D // by "db.coll": the documents of listIndexes
	counts      map[string]int64    // by "db.coll": the n of $collStats
	docs        map[string][]bson.D // by "db.coll": what $sample returns
	failList    string              // a command name that is answered with an error
}

func newWireServer(t *testing.T) *wireServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("no loopback to listen on: %v", err)
	}
	s := &wireServer{t: t, ln: ln}
	go s.accept()
	t.Cleanup(func() {
		if err := ln.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			slog.Warn("the fake server's listener did not close", "err", err)
		}
	})
	return s
}

func (s *wireServer) addr() (string, int) {
	a := s.ln.Addr().(*net.TCPAddr)
	return a.IP.String(), a.Port
}

func (s *wireServer) accept() {
	for {
		c, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.serve(c)
	}
}

// serve answers the messages of one connection until it closes.
func (s *wireServer) serve(c net.Conn) {
	defer func() {
		if err := c.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			slog.Warn("a connection of the fake server did not close", "err", err)
		}
	}()
	for {
		var head [16]byte
		if _, err := io.ReadFull(c, head[:]); err != nil {
			return
		}
		length := int(binary.LittleEndian.Uint32(head[0:4]))
		requestID := binary.LittleEndian.Uint32(head[4:8])
		opCode := binary.LittleEndian.Uint32(head[12:16])
		if length < 16 || length > 1<<24 {
			return
		}
		body := make([]byte, length-16)
		if _, err := io.ReadFull(c, body); err != nil {
			return
		}
		var cmd bson.D
		switch opCode {
		case 2004: // OP_QUERY: the handshake of a driver that starts with the legacy hello
			rest := body[4:]
			i := 0
			for rest[i] != 0 {
				i++
			}
			rest = rest[i+1+8:] // the name of the collection, skip and return
			if err := bson.Unmarshal(rest, &cmd); err != nil {
				return
			}
			reply := s.answer(cmd)
			s.replyLegacy(c, requestID, reply)
		case 2013: // OP_MSG
			flags := binary.LittleEndian.Uint32(body[0:4])
			rest := body[4:]
			if flags&1 != 0 {
				rest = rest[:len(rest)-4] // the checksum
			}
			if rest[0] != 0 {
				return
			}
			if err := bson.Unmarshal(rest[1:], &cmd); err != nil {
				return
			}
			reply := s.answer(cmd)
			s.replyMsg(c, requestID, reply)
		default:
			return
		}
	}
}

func (s *wireServer) replyMsg(c net.Conn, to uint32, doc bson.D) {
	data, err := bson.Marshal(doc)
	if err != nil {
		s.t.Errorf("fake server: %v", err)
		return
	}
	body := append([]byte{0, 0, 0, 0, 0}, data...) // no flags, a section of kind 0
	s.write(c, to, 2013, body)
}

func (s *wireServer) replyLegacy(c net.Conn, to uint32, doc bson.D) {
	data, err := bson.Marshal(doc)
	if err != nil {
		s.t.Errorf("fake server: %v", err)
		return
	}
	body := make([]byte, 20, 20+len(data))
	binary.LittleEndian.PutUint32(body[16:20], 1) // one document
	body = append(body, data...)
	s.write(c, to, 1, body)
}

func (s *wireServer) write(c net.Conn, to uint32, opCode uint32, body []byte) {
	msg := make([]byte, 16, 16+len(body))
	binary.LittleEndian.PutUint32(msg[0:4], uint32(16+len(body)))
	binary.LittleEndian.PutUint32(msg[8:12], to)
	binary.LittleEndian.PutUint32(msg[12:16], opCode)
	if _, err := c.Write(append(msg, body...)); err != nil {
		return
	}
}

func (s *wireServer) calls(name string) []bson.D {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []bson.D
	for _, c := range s.commands {
		if len(c) > 0 && c[0].Key == name {
			out = append(out, c)
		}
	}
	return out
}

func get(d bson.D, key string) (any, bool) {
	for _, e := range d {
		if e.Key == key {
			return e.Value, true
		}
	}
	return nil, false
}

func ok(fields ...bson.E) bson.D { return append(bson.D{{Key: "ok", Value: 1.0}}, fields...) }

func cursorReply(ns string, docs []bson.D) bson.D {
	batch := bson.A{}
	for _, d := range docs {
		batch = append(batch, d)
	}
	return ok(bson.E{Key: "cursor", Value: bson.D{{Key: "id", Value: int64(0)}, {Key: "ns", Value: ns}, {Key: "firstBatch", Value: batch}}})
}

// answer makes the reply to a command.
func (s *wireServer) answer(cmd bson.D) bson.D {
	if len(cmd) == 0 {
		return bson.D{{Key: "ok", Value: 0.0}, {Key: "errmsg", Value: "empty command"}, {Key: "code", Value: int32(9)}}
	}
	name := cmd[0].Key
	s.mu.Lock()
	defer s.mu.Unlock()
	s.commands = append(s.commands, cmd)
	if s.failList == name {
		return bson.D{{Key: "ok", Value: 0.0}, {Key: "errmsg", Value: "not authorized on shop to execute command " + name}, {Key: "code", Value: int32(13)}, {Key: "codeName", Value: "Unauthorized"}}
	}
	db, _ := get(cmd, "$db")
	dbName, _ := db.(string)
	switch name {
	case "isMaster", "ismaster", "hello":
		if client, found := get(cmd, "client"); found {
			if d, isDoc := client.(bson.D); isDoc {
				if app, found := get(d, "application"); found {
					if ad, isDoc := app.(bson.D); isDoc {
						n, _ := get(ad, "name")
						str, _ := n.(string)
						s.apps = append(s.apps, str)
					}
				}
			}
		}
		return ok(bson.E{Key: "ismaster", Value: true}, bson.E{Key: "isWritablePrimary", Value: true}, bson.E{Key: "helloOk", Value: true},
			bson.E{Key: "maxBsonObjectSize", Value: int32(16777216)}, bson.E{Key: "maxMessageSizeBytes", Value: int32(48000000)},
			bson.E{Key: "maxWriteBatchSize", Value: int32(100000)}, bson.E{Key: "minWireVersion", Value: int32(0)}, bson.E{Key: "maxWireVersion", Value: int32(21)},
			bson.E{Key: "logicalSessionTimeoutMinutes", Value: int32(30)}, bson.E{Key: "localTime", Value: bson.NewDateTimeFromTime(time.Now())})
	case "ping", "endSessions", "killCursors":
		return ok()
	case "listDatabases":
		return ok(bson.E{Key: "databases", Value: docsToA(s.databases)}, bson.E{Key: "totalSize", Value: int64(0)})
	case "listCollections":
		return cursorReply(dbName+".$cmd.listCollections", s.collections[dbName])
	case "listIndexes":
		coll, _ := cmd[0].Value.(string)
		return cursorReply(dbName+"."+coll, s.indexes[dbName+"."+coll])
	case "aggregate":
		coll, _ := cmd[0].Value.(string)
		pipeline, _ := get(cmd, "pipeline")
		stages, _ := pipeline.(bson.A)
		if len(stages) > 0 {
			if st, isDoc := stages[0].(bson.D); isDoc && len(st) > 0 {
				switch st[0].Key {
				case "$collStats":
					return cursorReply(dbName+"."+coll, []bson.D{{{Key: "_id", Value: nil}, {Key: "n", Value: s.counts[dbName+"."+coll]}}})
				case "$sample":
					return cursorReply(dbName+"."+coll, s.docs[dbName+"."+coll])
				}
			}
		}
		return cursorReply(dbName+"."+coll, nil)
	case "count":
		coll, _ := cmd[0].Value.(string)
		return ok(bson.E{Key: "n", Value: s.counts[dbName+"."+coll]})
	}
	return bson.D{{Key: "ok", Value: 0.0}, {Key: "errmsg", Value: "no such command: " + name}, {Key: "code", Value: int32(59)}, {Key: "codeName", Value: "CommandNotFound"}}
}

func docsToA(docs []bson.D) bson.A {
	a := bson.A{}
	for _, d := range docs {
		a = append(a, d)
	}
	return a
}

// fixtureWire makes a server with the same shop as fixtureBackend.
func fixtureWire(t *testing.T) *wireServer {
	t.Helper()
	s := newWireServer(t)
	s.databases = []bson.D{
		{{Key: "name", Value: "shop"}, {Key: "sizeOnDisk", Value: int64(12582912)}, {Key: "empty", Value: false}},
		{{Key: "name", Value: "old"}, {Key: "sizeOnDisk", Value: int64(0)}, {Key: "empty", Value: true}},
	}
	s.collections = map[string][]bson.D{"shop": {
		{{Key: "name", Value: "orders"}, {Key: "type", Value: "collection"}, {Key: "options", Value: bson.D{}}, {Key: "info", Value: bson.D{{Key: "readOnly", Value: false}}}},
		{{Key: "name", Value: "open_orders"}, {Key: "type", Value: "view"}, {Key: "options", Value: bson.D{{Key: "viewOn", Value: "orders"}, {Key: "pipeline", Value: bson.A{}}}}, {Key: "info", Value: bson.D{{Key: "readOnly", Value: true}}}},
		{{Key: "name", Value: "events"}, {Key: "type", Value: "timeseries"}, {Key: "options", Value: bson.D{{Key: "timeseries", Value: bson.D{{Key: "timeField", Value: "t"}}}}}, {Key: "info", Value: bson.D{{Key: "readOnly", Value: false}}}},
	}}
	s.indexes = map[string][]bson.D{"shop.orders": {
		{{Key: "v", Value: int32(2)}, {Key: "key", Value: bson.D{{Key: "_id", Value: int32(1)}}}, {Key: "name", Value: "_id_"}},
		{{Key: "v", Value: int32(2)}, {Key: "key", Value: bson.D{{Key: "sku", Value: int32(1)}, {Key: "created", Value: int32(-1)}}}, {Key: "name", Value: "sku_1_created_-1"},
			{Key: "unique", Value: true}, {Key: "sparse", Value: true}},
		{{Key: "v", Value: int32(2)}, {Key: "key", Value: bson.D{{Key: "created", Value: int32(1)}}}, {Key: "name", Value: "created_ttl"}, {Key: "expireAfterSeconds", Value: int32(3600)}},
	}}
	s.counts = map[string]int64{"shop.orders": 123456}
	s.docs = map[string][]bson.D{"shop.orders": {
		{{Key: "_id", Value: bson.ObjectID{1}}, {Key: "sku", Value: "A1"}, {Key: "qty", Value: int32(2)}},
		{{Key: "_id", Value: bson.ObjectID{2}}, {Key: "sku", Value: "B2"}, {Key: "qty", Value: int64(5)}},
	}}
	return s
}

// dialWire connects the real backend to the fake server.
func dialWire(t *testing.T, s *wireServer) Backend {
	t.Helper()
	host, port := s.addr()
	b, err := driverDialer{timeout: 3 * time.Second}.Dial(context.Background(), dbconn.Connection{Name: "Wire", Host: host, Port: port, Options: map[string]string{"directConnection": "true"}}, "")
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(func() {
		if err := b.Close(context.Background()); err != nil {
			t.Logf("Close: %v", err)
		}
	})
	return b
}

func TestTheRealBackendAgainstAServerOnTheWire(t *testing.T) {
	s := fixtureWire(t)
	b := dialWire(t, s)
	ctx := context.Background()

	if len(s.apps) == 0 || s.apps[0] != "Emetos" {
		t.Errorf("application names of the handshakes = %v", s.apps)
	}
	dbs, err := b.Databases(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(dbs) != 2 || dbs[0] != (DatabaseInfo{Name: "shop", SizeOnDisk: 12582912}) || dbs[1] != (DatabaseInfo{Name: "old", Empty: true}) {
		t.Errorf("databases = %+v", dbs)
	}
	// the databases the user is allowed to see are asked for, so that a user with no rights on the server sees its own
	if calls := s.calls("listDatabases"); len(calls) != 1 {
		t.Fatalf("%d listDatabases", len(calls))
	} else if v, _ := get(calls[0], "authorizedDatabases"); v != true {
		t.Errorf("authorizedDatabases = %v", v)
	}

	colls, err := b.Collections(ctx, "shop")
	if err != nil {
		t.Fatal(err)
	}
	want := []CollectionInfo{{Name: "orders", Kind: KindCollection}, {Name: "open_orders", Kind: KindView, ViewOn: "orders"}, {Name: "events", Kind: KindTimeSeries}}
	if len(colls) != len(want) {
		t.Fatalf("collections = %+v", colls)
	}
	for i := range want {
		if colls[i] != want[i] {
			t.Errorf("collection %d = %+v, want %+v", i, colls[i], want[i])
		}
	}

	n, err := b.EstimatedCount(ctx, "shop", "orders")
	if err != nil || n != 123456 {
		t.Errorf("EstimatedCount = %d, %v", n, err)
	}

	idx, err := b.Indexes(ctx, "shop", "orders")
	if err != nil {
		t.Fatal(err)
	}
	if len(idx) != 3 || idx[0].Name != "_id_" || !idx[1].Unique || !idx[1].Sparse || idx[2].TTLSeconds == nil || *idx[2].TTLSeconds != 3600 {
		t.Fatalf("indexes = %+v", idx)
	}
	if got := idx[1].Keys; len(got) != 2 || got[0].Key != "sku" || got[0].Value != int32(1) || got[1].Key != "created" || got[1].Value != int32(-1) {
		t.Errorf("keys = %+v", got)
	}

	docs, err := b.Sample(ctx, "shop", "orders", 25)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 2 || docs[0][1].Value != "A1" || docs[1][2].Value != int64(5) {
		t.Errorf("sample = %+v", docs)
	}
	var sampled bool
	for _, c := range s.calls("aggregate") {
		pipeline, _ := get(c, "pipeline")
		stages, _ := pipeline.(bson.A)
		if st, isDoc := stages[0].(bson.D); isDoc && st[0].Key == "$sample" {
			size, _ := get(st[0].Value.(bson.D), "size")
			sampled = size == int32(25) || size == int64(25)
		}
	}
	if !sampled {
		t.Error("no $sample of 25 documents was sent")
	}
}

func TestTheExplorerOverAServerOnTheWire(t *testing.T) {
	s := fixtureWire(t)
	r := newRig(t)
	r.ext.mgr.dialer = driverDialer{timeout: 3 * time.Second}
	host, port := s.addr()
	c := r.save(dbconn.Connection{Name: "Wire", Engine: EngineID, Host: host, Port: port, Options: map[string]string{"directConnection": "true"}})
	if err := r.dbAPI.Run(dbconn.Connect, c.ID); err != nil {
		t.Fatal(err)
	}
	r.dbAPI.WaitFor("no password to ask", func() bool { return len(r.dbAPI.Inputs()) == 1 })
	r.dbAPI.Inputs()[0].Accept("")
	r.dbAPI.WaitFor("the connection", func() bool { return r.hub.State(c.ID).Status == dbconn.Connected })
	shop := find(t, r.children(c, nil), "shop")
	nodes := r.children(c, &shop)
	orders := find(t, nodes, "orders")
	if got := labels(nodes); len(got) != 3 || orders.Description != "123,456 documents" || orders.Tooltip != "orders: about 123,456 documents (estimated from the metadata of the collection)" {
		t.Errorf("collections = %v (%+v)", got, orders)
	}
	groups := r.children(c, &orders)
	schema := find(t, groups, "Schema")
	fields := r.children(c, &schema)
	if len(fields) != 3 || fields[2].Description != "int | long" {
		t.Errorf("fields = %+v", fields)
	}
	// the server says no: its message comes to the user
	s.mu.Lock()
	s.failList = "listIndexes"
	s.mu.Unlock()
	indexes := find(t, groups, "Indexes")
	_, err := r.hub.Children(context.Background(), c.ID, &indexes)
	if err == nil || !strings.Contains(err.Error(), "not authorized") {
		t.Errorf("err = %v", err)
	}
	wantRoute(t, err, "driverBackend.Indexes.MongoListIndexesFailed")
	if err := r.dbAPI.Run(dbconn.Disconnect, c.ID); err != nil {
		t.Fatal(err)
	}
}
