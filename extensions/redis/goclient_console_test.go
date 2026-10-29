package redis

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"
)

// dialRESP opens the Go client on a RESP server over a fake store; prep changes the server first.
func dialRESP(t *testing.T, store *fakeServer, password string, prep func(*respServer)) (Server, *respServer) {
	t.Helper()
	srv := newRESPServer(t, store, password)
	if prep != nil {
		prep(srv)
	}
	store.resp3 = srv.resp3
	s, err := GoDialer{}.Dial(context.Background(), Spec{Mode: ModeStandalone, Addrs: []string{srv.addr()}, Password: password, DialTimeout: 2 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Logf("close: %v", err)
		}
	})
	return s, srv
}

func TestDoReadsTheRepliesOfRESP2(t *testing.T) {
	s, _ := dialRESP(t, fixtureServer(), "", nil)
	db, err := s.DB(0)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	tests := []struct {
		args []string
		want Reply
	}{
		{[]string{"PING"}, Reply{Kind: ReplyStatus, Str: "PONG"}},
		{[]string{"GET", "greeting"}, Reply{Kind: ReplyString, Str: "hello world"}},
		{[]string{"GET", "nothing"}, Reply{Kind: ReplyNil}},
		{[]string{"DBSIZE"}, Reply{Kind: ReplyInt, Int: 9}},
		{[]string{"HGETALL", "user:7:profile"}, Reply{Kind: ReplyArray, Items: []Reply{str("name"), str("Bob")}}},
		{[]string{"SMEMBERS", "user:42:tags"}, Reply{Kind: ReplyArray, Items: []Reply{str("go"), str("redis"), str("ui")}}},
		{[]string{"ECHO", "a b\nc"}, str("a b\nc")},
		{[]string{"GET", "user:7:profile"}, Reply{Kind: ReplyError, Str: "WRONGTYPE Operation against a key holding the wrong kind of value"}},
		{[]string{"NOPE"}, Reply{Kind: ReplyError, Str: "ERR unknown command 'NOPE'"}},
		{[]string{"TEST.TYPES"}, arr(Reply{Kind: ReplyNil}, num(7), str("text"), str("1.5"), num(1), str("123456789012345678901234567890"),
			arr(str("k"), str("v")), arr(str("m")), Reply{Kind: ReplyError, Str: "WRONGTYPE inside"})},
	}
	for _, tc := range tests {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			got, err := db.Do(ctx, tc.args)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %+v\nwant %+v", got, tc.want)
			}
		})
	}
}

func TestDoReadsTheRepliesOfRESP3(t *testing.T) {
	s, _ := dialRESP(t, fixtureServer(), "", func(srv *respServer) { srv.resp3 = true })
	srv3 := s
	db, err := srv3.DB(0)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	tests := []struct {
		args []string
		want Reply
	}{
		{[]string{"HGETALL", "user:7:profile"}, Reply{Kind: ReplyMap, Pairs: []ReplyPair{{str("name"), str("Bob")}}}},
		{[]string{"SMEMBERS", "user:42:tags"}, Reply{Kind: ReplySet, Items: []Reply{str("go"), str("redis"), str("ui")}}},
		{[]string{"LRANGE", "queue:jobs", "0", "-1"}, arr(str("build"), str("test"), str("deploy"))},
		{[]string{"GET", "nothing"}, Reply{Kind: ReplyNil}},
		{[]string{"TEST.TYPES"}, arr(Reply{Kind: ReplyNil}, num(7), str("text"), Reply{Kind: ReplyDouble, Float: 1.5}, Reply{Kind: ReplyBool, Bool: true},
			Reply{Kind: ReplyBigNumber, Str: "123456789012345678901234567890"}, Reply{Kind: ReplyMap, Pairs: []ReplyPair{{str("k"), str("v")}}},
			arr(str("m")), str("hello world"), Reply{Kind: ReplyError, Str: "WRONGTYPE inside"})},
	}
	for _, tc := range tests {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			got, err := db.Do(ctx, tc.args)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %+v\nwant %+v", got, tc.want)
			}
		})
	}
}

func TestDoShowsAMapAsAMapOnlyOnRESP3(t *testing.T) {
	// the same command on a RESP2 server is a flat list
	s, _ := dialRESP(t, fixtureServer(), "", nil)
	db, _ := s.DB(0)
	rep, err := db.Do(context.Background(), []string{"HGETALL", "user:42:profile"})
	if err != nil || rep.Kind != ReplyArray || len(rep.Items) != 6 {
		t.Errorf("RESP2: %+v, %v", rep, err)
	}
}

func TestDoAsksForARESP3SetOnlyOnARESP3Connection(t *testing.T) {
	s, srv := dialRESP(t, fixtureServer(), "", func(srv *respServer) { srv.resp3 = true })
	db, _ := s.DB(0)
	for i := 0; i < 3; i++ {
		if _, err := db.Do(context.Background(), []string{"SMEMBERS", "user:42:tags"}); err != nil {
			t.Fatal(err)
		}
	}
	hellos := 0
	for _, c := range srv.commands() {
		if c == "HELLO" {
			hellos++
		}
	}
	if hellos != 1 {
		t.Errorf("the protocol was probed %d times: %q", hellos, srv.commands())
	}
}

func TestDoWithAPassword(t *testing.T) {
	for _, resp3 := range []bool{false, true} {
		s, _ := dialRESP(t, fixtureServer(), "s3cret", func(srv *respServer) { srv.resp3 = resp3 })
		db, _ := s.DB(0)
		rep, err := db.Do(context.Background(), []string{"GET", "greeting"})
		if err != nil || rep.Str != "hello world" {
			t.Errorf("resp3=%v: %+v, %v", resp3, rep, err)
		}
	}
}

func TestDoStopsWhenTheContextEnds(t *testing.T) {
	s, srv := dialRESP(t, fixtureServer(), "", nil)
	db, _ := s.DB(0)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := db.Do(ctx, []string{"BLPOP", "queue:none", "0"})
		done <- err
	}()
	deadline := time.Now().Add(5 * time.Second)
	for !contains(srv.commands(), "BLPOP queue:none 0") {
		if time.Now().After(deadline) {
			t.Fatal("the command did not arrive")
		}
		time.Sleep(5 * time.Millisecond)
	}
	start := time.Now()
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Error("a cancelled command succeeded")
		}
		wantRoute(t, err, "goDB.Do.RedisDoFailed")
	case <-time.After(5 * time.Second):
		t.Fatal("the command does not stop when its context is cancelled")
	}
	if time.Since(start) > 3*time.Second {
		t.Errorf("it took %v to stop", time.Since(start))
	}
	// the client works after a cancel
	if rep, err := db.Do(context.Background(), []string{"PING"}); err != nil || rep.Str != "PONG" {
		t.Errorf("PING after the cancel: %+v, %v", rep, err)
	}
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if strings.EqualFold(s, want) {
			return true
		}
	}
	return false
}

func TestDoFailuresHaveTheirTags(t *testing.T) {
	s, _ := dialRESP(t, fixtureServer(), "", nil)
	db, _ := s.DB(0)
	_, err := db.Do(context.Background(), nil)
	wantRoute(t, err, "goDB.Do.RedisCommandEmpty")
	// a connection that breaks
	_, err = db.Do(context.Background(), []string{"TEST.DROP"})
	wantRoute(t, err, "goDB.Do.RedisDoFailed")
	if appErr, ok := asTagged(err); !ok || appErr.Metadata["command"] != "TEST.DROP" {
		t.Errorf("metadata = %+v", appErr)
	}
}

func TestPubSubThroughTheRealClient(t *testing.T) {
	s, srv := dialRESP(t, fixtureServer(), "", nil)
	ctx := context.Background()
	sub, err := s.PubSub(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()
	if err := sub.Subscribe(ctx, "news"); err != nil {
		t.Fatal(err)
	}
	if err := sub.PSubscribe(ctx, "sport.*"); err != nil {
		t.Fatal(err)
	}
	srv.waitListening(t, "news")
	srv.waitListening(t, "sport.*")
	db, _ := s.DB(0)
	if rep, err := db.Do(ctx, []string{"PUBLISH", "news", "hello"}); err != nil || rep.Int != 1 {
		t.Fatalf("PUBLISH: %+v, %v", rep, err)
	}
	if rep, err := db.Do(ctx, []string{"PUBLISH", "sport.golf", "birdie"}); err != nil || rep.Int != 1 {
		t.Fatalf("PUBLISH: %+v, %v", rep, err)
	}
	var got []PubSubMessage
	for len(got) < 2 {
		select {
		case m, ok := <-sub.Messages():
			if !ok {
				t.Fatalf("the messages ended: %v", sub.Err())
			}
			got = append(got, m)
		case <-time.After(5 * time.Second):
			t.Fatalf("got %+v", got)
		}
	}
	if got[0].Channel != "news" || got[0].Payload != "hello" || got[0].Pattern != "" || got[0].Time.IsZero() {
		t.Errorf("first = %+v", got[0])
	}
	if got[1].Channel != "sport.golf" || got[1].Pattern != "sport.*" || got[1].Payload != "birdie" {
		t.Errorf("second = %+v", got[1])
	}
	if err := sub.Unsubscribe(ctx, "news"); err != nil {
		t.Fatal(err)
	}
	if err := sub.PUnsubscribe(ctx, "sport.*"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"SUBSCRIBE news", "PSUBSCRIBE sport.*", "UNSUBSCRIBE news", "PUNSUBSCRIBE sport.*"} {
		deadline := time.Now().Add(2 * time.Second)
		for !contains(srv.commands(), want) && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}
		if !contains(srv.commands(), want) {
			t.Errorf("%q was not sent: %q", want, srv.commands())
		}
	}
	sub.Close()
	sub.Close() // twice is fine
	select {
	case _, ok := <-sub.Messages():
		if ok {
			t.Error("a message after Close")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the messages do not end after Close")
	}
	if sub.Err() != nil {
		t.Errorf("Err after Close = %v", sub.Err())
	}
}

func TestPubSubFailuresHaveTheirTags(t *testing.T) {
	s, srv := dialRESP(t, fixtureServer(), "", nil)
	ctx := context.Background()
	sub, err := s.PubSub(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// the server breaks the connection of the subscription
	if err := sub.Subscribe(ctx, "news"); err != nil {
		t.Fatal(err)
	}
	srv.waitListening(t, "news")
	srv.dropSubscribers()
	select {
	case _, ok := <-sub.Messages():
		if ok {
			t.Fatal("a message")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the messages do not end when the connection breaks")
	}
	wantRoute(t, sub.Err(), "goSub.run.RedisPubSubFailed")
	// a closed one refuses
	sub.Close()
	wantRoute(t, sub.Subscribe(ctx, "x"), "goSub.Subscribe.RedisSubscribeFailed")
	wantRoute(t, sub.PSubscribe(ctx, "x"), "goSub.PSubscribe.RedisPSubscribeFailed")
	wantRoute(t, sub.Unsubscribe(ctx, "x"), "goSub.Unsubscribe.RedisUnsubscribeFailed")
	wantRoute(t, sub.PUnsubscribe(ctx, "x"), "goSub.PUnsubscribe.RedisPUnsubscribeFailed")
}

func TestMonitorThroughTheRealClient(t *testing.T) {
	for _, password := range []string{"", "s3cret"} {
		s, _ := dialRESP(t, fixtureServer(), password, nil)
		ctx := context.Background()
		mon, err := s.Monitor(ctx)
		if err != nil {
			t.Fatal(err)
		}
		db, _ := s.DB(0)
		if _, err := db.Do(ctx, []string{"ECHO", "traced"}); err != nil {
			t.Fatal(err)
		}
		select {
		case line, ok := <-mon.Lines():
			if !ok {
				t.Fatalf("the lines ended: %v", mon.Err())
			}
			l := ParseMonitorLine(line)
			if l.DB != "0" || !strings.HasPrefix(l.Client, "127.0.0.1:") || l.Command != `"ECHO" "traced"` || l.Time.Unix() != 1700000000 {
				t.Errorf("line %q parsed as %+v", line, l)
			}
			if strings.Contains(line, "s3cret") {
				t.Errorf("the password is in the line: %q", line)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("no line")
		}
		mon.Close()
		mon.Close() // twice is fine
		select {
		case _, ok := <-mon.Lines():
			if ok {
				t.Error("a line after Close")
			}
		case <-time.After(5 * time.Second):
			t.Fatal("the lines do not end after Close")
		}
		if mon.Err() != nil {
			t.Errorf("Err after Close = %v", mon.Err())
		}
	}
}

func TestMonitorFailuresHaveTheirTags(t *testing.T) {
	// a wrong password
	store := fixtureServer()
	srv := newRESPServer(t, store, "s3cret")
	good := Spec{Mode: ModeStandalone, Addrs: []string{srv.addr()}, Password: "s3cret", DialTimeout: 2 * time.Second}
	s, err := GoDialer{}.Dial(context.Background(), good)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := s.Close(); err != nil {
			t.Logf("close: %v", err)
		}
	}()
	s.(*goServer).spec.Password = "wrong"
	_, err = s.Monitor(context.Background())
	wantRoute(t, err, "roundTrip.RedisMonitorRefused")
	if appErr, ok := asTagged(err); !ok || appErr.Metadata["reply"] != "WRONGPASS" || strings.Contains(appErr.Message, "wrong") {
		t.Errorf("error = %+v", appErr)
	}
	// a server that is not there
	s.(*goServer).spec.Addrs = []string{"127.0.0.1:1"}
	s.(*goServer).spec.DialTimeout = 300 * time.Millisecond
	_, err = s.Monitor(context.Background())
	wantRoute(t, err, "goServer.Monitor.RedisMonitorDialFailed")
	// a Sentinel and a cluster have none
	s.(*goServer).spec.Mode = ModeCluster
	_, err = s.Monitor(context.Background())
	wantRoute(t, err, "goServer.Monitor.RedisMonitorUnsupported")
}

func TestMonitorEndsWhenTheServerGoes(t *testing.T) {
	s, srv := dialRESP(t, fixtureServer(), "", nil)
	mon, err := s.Monitor(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer mon.Close()
	srv.dropMonitors()
	select {
	case _, ok := <-mon.Lines():
		if ok {
			t.Fatal("a line")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the lines do not end")
	}
	wantRoute(t, mon.Err(), "goMonitor.run.RedisMonitorFailed")
}

// dropSubscribers breaks the connections in Pub/Sub mode; dropMonitors those in MONITOR mode.
func (s *respServer) dropSubscribers() {
	s.mu.Lock()
	cs := append([]*respConn(nil), s.subs...)
	s.mu.Unlock()
	for _, c := range cs {
		_ = c.conn.Close() // the test breaks the connection on purpose
	}
}

func (s *respServer) dropMonitors() {
	s.mu.Lock()
	cs := append([]*respConn(nil), s.mons...)
	s.mu.Unlock()
	for _, c := range cs {
		_ = c.conn.Close() // the test breaks the connection on purpose
	}
}

// waitListening waits until a connection listens to the channel or pattern.
func (s *respServer) waitListening(t *testing.T, name string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		s.mu.Lock()
		found := false
		for _, c := range s.subs {
			found = found || c.channels[name] || c.patterns[name]
		}
		s.mu.Unlock()
		if found {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("nobody listens to %q", name)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
