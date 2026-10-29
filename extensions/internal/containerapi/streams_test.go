package containerapi_test

import (
	"context"
	"encoding/binary"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extensions/internal/containerapi"
	"github.com/rm4n0s/emetos/extensions/internal/containerapi/containerapitest"
	"github.com/rm4n0s/errors"
)

// collect runs Events in a goroutine and returns what it needs: the events as they come, the result of the call when it
// ends, and a function that cancels it.
type eventRun struct {
	events chan containerapi.Event
	done   chan error
	cancel context.CancelFunc
	ready  chan struct{}
}

func runEvents(t *testing.T, c *containerapi.Client, opts containerapi.EventsOptions, fn func(containerapi.Event) error) *eventRun {
	t.Helper()
	ctx, cancel := context.WithCancel(bg)
	r := &eventRun{events: make(chan containerapi.Event, 256), done: make(chan error, 1), cancel: cancel, ready: make(chan struct{})}
	t.Cleanup(cancel)
	var once sync.Once
	opts.Ready = func() { once.Do(func() { close(r.ready) }) }
	go func() {
		r.done <- c.Events(ctx, opts, func(ev containerapi.Event) error {
			if fn != nil {
				if err := fn(ev); err != nil {
					return err
				}
			}
			r.events <- ev
			return nil
		})
	}()
	select {
	case <-r.ready:
	case err := <-r.done:
		t.Fatalf("Events ended before it was ready: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("Events was not ready")
	}
	return r
}

func (r *eventRun) next(t *testing.T) containerapi.Event {
	t.Helper()
	select {
	case ev := <-r.events:
		return ev
	case err := <-r.done:
		t.Fatalf("Events ended (%v) while an event was expected", err)
	case <-time.After(5 * time.Second):
		t.Fatal("no event arrived")
	}
	return containerapi.Event{}
}

func (r *eventRun) end(t *testing.T) error {
	t.Helper()
	select {
	case err := <-r.done:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("Events did not end")
	}
	return nil
}

func TestEventsFollowTheEngine(t *testing.T) {
	for _, kind := range kinds {
		t.Run(string(kind), func(t *testing.T) {
			_, c := engine(t, kind)
			r := runEvents(t, c, containerapi.EventsOptions{Filters: containerapi.Filters{"type": {"container"}}}, nil)
			res, err := c.CreateContainer(bg, containerapi.ContainerSpec{Name: "ev", Image: containerapitest.ImageBusyBox, Labels: map[string]string{"k": "v"}, Cmd: []string{"sh", "-c", "echo hi"}})
			if err != nil {
				t.Fatal(err)
			}
			if err := c.StartContainer(bg, res.ID); err != nil {
				t.Fatal(err)
			}
			if err := c.StopContainer(bg, res.ID, 0); err != nil {
				t.Fatal(err)
			}
			if err := c.RemoveContainer(bg, res.ID, containerapi.RemoveOptions{}); err != nil {
				t.Fatal(err)
			}
			die, destroy := "die", "destroy"
			if kind == containerapi.KindPodman {
				die, destroy = "died", "remove"
			}
			var got []string
			for len(got) < 5 {
				ev := r.next(t)
				if ev.Type != "container" || ev.ID != res.ID || ev.Attributes["name"] != "ev" || ev.Attributes["k"] != "v" || ev.Time.IsZero() || ev.Scope != "local" {
					t.Errorf("event = %+v", ev)
				}
				got = append(got, ev.Action)
			}
			if want := []string{"create", "start", die, "stop", destroy}; !reflect.DeepEqual(got, want) {
				t.Errorf("actions = %v, want %v", got, want)
			}
			r.cancel()
			if err := r.end(t); err != nil {
				t.Errorf("Events after cancel = %v, want nil", err)
			}
		})
	}
}

func TestEventsFiltersAndReplay(t *testing.T) {
	f, c := engine(t, containerapi.KindDocker)
	f.AddContainer("old", containerapitest.ImageBusyBox, nil, "running")
	past := time.Now().Add(-time.Hour)
	f.Emit(containerapi.Event{Type: "container", Action: "start", ID: "c1", Attributes: map[string]string{"name": "one", "dev.emetos.test": "1"}, Time: past.Add(time.Second)})
	f.Emit(containerapi.Event{Type: "image", Action: "pull", ID: "i1", Attributes: map[string]string{"name": "img"}, Time: past.Add(2 * time.Second)})
	f.Emit(containerapi.Event{Type: "container", Action: "die", ID: "c2", Attributes: map[string]string{"name": "two"}, Time: past.Add(3 * time.Second)})
	f.Emit(containerapi.Event{Type: "container", Action: "start", ID: "c3", Attributes: map[string]string{"name": "three"}, Time: past.Add(4 * time.Second)})
	t.Run("a closed range is replayed and ends by itself", func(t *testing.T) {
		var actions []string
		err := c.Events(bg, containerapi.EventsOptions{Since: past, Until: past.Add(3 * time.Second), Filters: containerapi.Filters{"type": {"container"}}}, func(ev containerapi.Event) error {
			actions = append(actions, ev.Action+":"+ev.ID)
			return nil
		})
		if err != nil {
			t.Fatalf("Events over a closed range = %v, want nil", err)
		}
		if want := []string{"start:c1", "die:c2"}; !reflect.DeepEqual(actions, want) {
			t.Errorf("events = %v, want %v", actions, want)
		}
		q := queryOf(t, lastRequest(t, f, http.MethodGet, "/events"))
		if q.Get("since") == "" || q.Get("until") == "" {
			t.Errorf("query = %v", q)
		}
	})
	t.Run("filters by event and by label", func(t *testing.T) {
		var ids []string
		err := c.Events(bg, containerapi.EventsOptions{Since: past, Until: past.Add(time.Minute), Filters: containerapi.Filters{"event": {"start"}, "label": {"dev.emetos.test=1"}}}, func(ev containerapi.Event) error {
			ids = append(ids, ev.ID)
			return nil
		})
		if err != nil || !reflect.DeepEqual(ids, []string{"c1"}) {
			t.Errorf("events = %v, %v; want c1", ids, err)
		}
	})
}

func TestEventsStopWhenTheCallbackFails(t *testing.T) {
	f, c := engine(t, containerapi.KindDocker)
	boom := &errors.Error{Tag: "TestCallbackStop"}
	r := runEvents(t, c, containerapi.EventsOptions{}, func(containerapi.Event) error { return boom })
	f.Emit(containerapi.Event{Type: "container", Action: "start", ID: "x"})
	if err := r.end(t); !errors.Is(err, boom) {
		t.Errorf("Events = %v, want the callback's error", err)
	}
}

func TestEventsEndedByTheEngineIsAnError(t *testing.T) {
	f, c := engine(t, containerapi.KindDocker)
	if _, err := c.Negotiate(bg); err != nil {
		t.Fatal(err)
	}
	f.Override(http.MethodGet, "/events", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"Type":"container","Action":"start","Actor":{"ID":"a"}}` + "\n"))
	})
	var seen []string
	err := c.Events(bg, containerapi.EventsOptions{}, func(ev containerapi.Event) error { seen = append(seen, ev.ID); return nil })
	wantRoute(t, err, "Client.Events.ContainerAPIStreamEnded")
	if !reflect.DeepEqual(seen, []string{"a"}) {
		t.Errorf("events = %v", seen)
	}
	// the same stream with an Until is a normal end
	if err := c.Events(bg, containerapi.EventsOptions{Until: time.Now()}, func(containerapi.Event) error { return nil }); err != nil {
		t.Errorf("a stream with Until that ends = %v, want nil", err)
	}
}

func TestEventsOldFormatAndBadLines(t *testing.T) {
	f, c := engine(t, containerapi.KindDocker)
	if _, err := c.Negotiate(bg); err != nil {
		t.Fatal(err)
	}
	f.Override(http.MethodGet, "/events", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("\n" + `{"status":"start","id":"old1","time":1793229025}` + "\n"))
	})
	var got []containerapi.Event
	err := c.Events(bg, containerapi.EventsOptions{Until: time.Now()}, func(ev containerapi.Event) error { got = append(got, ev); return nil })
	if err != nil || len(got) != 1 || got[0].Action != "start" || got[0].ID != "old1" || got[0].Time.Unix() != 1793229025 {
		t.Errorf("old-format event = %+v, %v", got, err)
	}
	f2, c2 := engine(t, containerapi.KindDocker)
	if _, err := c2.Negotiate(bg); err != nil {
		t.Fatal(err)
	}
	f2.Override(http.MethodGet, "/events", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("not json\n")) })
	wantRoute(t, c2.Events(bg, containerapi.EventsOptions{}, func(containerapi.Event) error { return nil }), "Client.Events->scanLines->Client.Events.func2.ContainerAPIDecodeFailed")
	f3, c3 := engine(t, containerapi.KindDocker)
	f3.FailNext(http.MethodGet, "/events", http.StatusInternalServerError, "events unavailable")
	wantRoute(t, c3.Events(bg, containerapi.EventsOptions{}, func(containerapi.Event) error { return nil }), "Client.Events->Client.do.ContainerAPIEngineError")
}

func TestEventsWithManyConsumers(t *testing.T) {
	f, c := engine(t, containerapi.KindDocker)
	runs := make([]*eventRun, 4)
	for i := range runs {
		runs[i] = runEvents(t, c, containerapi.EventsOptions{}, nil)
	}
	f.Emit(containerapi.Event{Type: "container", Action: "start", ID: "shared"})
	for i, r := range runs {
		if ev := r.next(t); ev.ID != "shared" {
			t.Errorf("consumer %d got %+v", i, ev)
		}
		r.cancel()
	}
	for _, r := range runs {
		if err := r.end(t); err != nil {
			t.Error(err)
		}
	}
}

// logFixture makes a running container with some output on both streams.
func logFixture(t *testing.T, kind containerapi.Kind) (*containerapitest.Fake, *containerapi.Client, string) {
	t.Helper()
	f, c := engine(t, kind)
	res, err := c.CreateContainer(bg, containerapi.ContainerSpec{Name: "logs", Image: containerapitest.ImageBusyBox, Cmd: []string{"sh", "-c", "echo one; echo oops >&2; echo two"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.StartContainer(bg, res.ID); err != nil {
		t.Fatal(err)
	}
	return f, c, res.ID
}

func readLogs(t *testing.T, c *containerapi.Client, id string, opts containerapi.LogsOptions) []containerapi.LogLine {
	t.Helper()
	var lines []containerapi.LogLine
	if err := c.Logs(bg, id, opts, func(l containerapi.LogLine) error { lines = append(lines, l); return nil }); err != nil {
		t.Fatal(err)
	}
	return lines
}

func TestLogs(t *testing.T) {
	for _, kind := range kinds {
		t.Run(string(kind), func(t *testing.T) {
			f, c, id := logFixture(t, kind)
			text := func(ls []containerapi.LogLine) []string {
				var out []string
				for _, l := range ls {
					out = append(out, l.Stream.String()+":"+l.Text)
				}
				return out
			}
			all := readLogs(t, c, id, containerapi.LogsOptions{})
			if want := []string{"stdout:one", "stderr:oops", "stdout:two"}; !reflect.DeepEqual(text(all), want) {
				t.Errorf("logs = %v, want %v", text(all), want)
			}
			if q := queryOf(t, lastRequest(t, f, http.MethodGet, "/logs")); q.Get("stdout") != "true" || q.Get("stderr") != "true" || q.Has("follow") || q.Has("timestamps") || q.Has("tail") {
				t.Errorf("default query = %v", q)
			}
			onlyErr := readLogs(t, c, id, containerapi.LogsOptions{Stderr: true})
			if want := []string{"stderr:oops"}; !reflect.DeepEqual(text(onlyErr), want) {
				t.Errorf("stderr only = %v", text(onlyErr))
			}
			tail := readLogs(t, c, id, containerapi.LogsOptions{Tail: 2})
			if want := []string{"stderr:oops", "stdout:two"}; !reflect.DeepEqual(text(tail), want) {
				t.Errorf("tail 2 = %v, want %v", text(tail), want)
			}
			stamped := readLogs(t, c, id, containerapi.LogsOptions{Timestamps: true, Since: time.Now().Add(-time.Hour)})
			for _, l := range stamped {
				if l.Time.IsZero() || time.Since(l.Time) > time.Minute || strings.Contains(l.Text, " ") && strings.HasPrefix(l.Text, "20") {
					t.Errorf("stamped line = %+v", l)
				}
			}
			if want := []string{"stdout:one", "stderr:oops", "stdout:two"}; !reflect.DeepEqual(text(stamped), want) {
				t.Errorf("stamped = %v", text(stamped))
			}
			if q := queryOf(t, lastRequest(t, f, http.MethodGet, "/logs")); q.Get("timestamps") != "true" || q.Get("since") == "" {
				t.Errorf("stamped query = %v", q)
			}
			// a line with a space in front that is not a timestamp stays whole
			f.Log(id, containerapi.StreamStdout, "plain text, no stamp")
			last := readLogs(t, c, id, containerapi.LogsOptions{Tail: 1})
			if len(last) != 1 || last[0].Text != "plain text, no stamp" || !last[0].Time.IsZero() {
				t.Errorf("last = %+v", last)
			}
		})
	}
}

func TestLogsFollowSeesNewLinesAndEndsWhenTheContainerStops(t *testing.T) {
	for _, kind := range kinds {
		t.Run(string(kind), func(t *testing.T) {
			f, c, id := logFixture(t, kind)
			lines := make(chan string, 16)
			done := make(chan error, 1)
			go func() {
				done <- c.Logs(bg, id, containerapi.LogsOptions{Follow: true}, func(l containerapi.LogLine) error { lines <- l.Text; return nil })
			}()
			for _, want := range []string{"one", "oops", "two"} {
				select {
				case got := <-lines:
					if got != want {
						t.Fatalf("line = %q, want %q", got, want)
					}
				case <-time.After(5 * time.Second):
					t.Fatalf("no %q line", want)
				}
			}
			f.Log(id, containerapi.StreamStdout, "later")
			select {
			case got := <-lines:
				if got != "later" {
					t.Fatalf("line = %q", got)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("no new line while following")
			}
			if err := c.StopContainer(bg, id, 0); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-done:
				if err != nil {
					t.Errorf("Logs after the container stopped = %v, want nil", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("Logs did not end when the container stopped")
			}
		})
	}
}

func TestLogsEndWithTheContext(t *testing.T) {
	_, c, id := logFixture(t, containerapi.KindDocker)
	ctx, cancel := context.WithCancel(bg)
	got := make(chan struct{}, 1)
	done := make(chan error, 1)
	go func() {
		done <- c.Logs(ctx, id, containerapi.LogsOptions{Follow: true}, func(containerapi.LogLine) error {
			select {
			case got <- struct{}{}:
			default:
			}
			return nil
		})
	}()
	<-got
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Logs after cancel = %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Logs did not end")
	}
}

func TestLogsStopWhenTheCallbackFails(t *testing.T) {
	_, c, id := logFixture(t, containerapi.KindDocker)
	boom := &errors.Error{Tag: "TestCallbackEnough"}
	n := 0
	err := c.Logs(bg, id, containerapi.LogsOptions{}, func(containerapi.LogLine) error { n++; return boom })
	if !errors.Is(err, boom) || n != 1 {
		t.Errorf("Logs = %v after %d lines", err, n)
	}
}

func TestLogsOfAContainerWithATerminalAreRaw(t *testing.T) {
	for _, kind := range kinds {
		t.Run(string(kind), func(t *testing.T) {
			f, c := engine(t, kind)
			res, err := c.CreateContainer(bg, containerapi.ContainerSpec{Name: "tty", Image: containerapitest.ImageBusyBox, Cmd: []string{"sh", "-c", "echo shell"}, Tty: true})
			if err != nil {
				t.Fatal(err)
			}
			if err := c.StartContainer(bg, res.ID); err != nil {
				t.Fatal(err)
			}
			f.Log(res.ID, containerapi.StreamStdout, "second\r")
			var got []containerapi.LogLine
			if err := c.Logs(bg, res.ID, containerapi.LogsOptions{}, func(l containerapi.LogLine) error { got = append(got, l); return nil }); err != nil {
				t.Fatal(err)
			}
			if len(got) != 2 || got[0].Text != "shell" || got[1].Text != "second" || got[0].Stream != containerapi.StreamStdout {
				t.Errorf("tty logs = %+v", got)
			}
		})
	}
}

func TestLogsKeepTheEndOfALineWithoutALineEnd(t *testing.T) {
	f, c := engine(t, containerapi.KindDocker)
	res, err := c.CreateContainer(bg, containerapi.ContainerSpec{Name: "noeol", Image: containerapitest.ImageBusyBox})
	if err != nil {
		t.Fatal(err)
	}
	f.Override(http.MethodGet, "/logs", func(w http.ResponseWriter, _ *http.Request) {
		writeRawFrames(w, [][]byte{frame(1, "first\npart"), frame(1, "ial"), frame(2, "no newline either")})
	})
	var got []string
	if err := c.Logs(bg, res.ID, containerapi.LogsOptions{}, func(l containerapi.LogLine) error { got = append(got, l.Stream.String()+":"+l.Text); return nil }); err != nil {
		t.Fatal(err)
	}
	// the unfinished part of each stream is flushed at the end, stdout before stderr
	if want := []string{"stdout:first", "stdout:partial", "stderr:no newline either"}; !reflect.DeepEqual(got, want) {
		t.Errorf("lines = %v, want %v", got, want)
	}
}

func frame(stream byte, data string) []byte {
	out := make([]byte, 8, 8+len(data))
	out[0] = stream
	binary.BigEndian.PutUint32(out[4:], uint32(len(data)))
	return append(out, data...)
}

func writeRawFrames(w http.ResponseWriter, frames [][]byte) {
	for _, f := range frames {
		_, _ = w.Write(f)
	}
}

func TestLogsProblems(t *testing.T) {
	_, c := engine(t, containerapi.KindDocker)
	_, err := c.CreateContainer(bg, containerapi.ContainerSpec{Name: "p", Image: containerapitest.ImageBusyBox})
	if err != nil {
		t.Fatal(err)
	}
	wantRoute(t, c.Logs(bg, "ghost", containerapi.LogsOptions{}, nil), "Client.Logs->Client.InspectContainer->Client.call->Client.do.ContainerAPINotFound")

	t.Run("a damaged stream", func(t *testing.T) {
		f, c := engine(t, containerapi.KindDocker)
		res, _ := c.CreateContainer(bg, containerapi.ContainerSpec{Name: "p", Image: containerapitest.ImageBusyBox})
		f.Override(http.MethodGet, "/logs", func(w http.ResponseWriter, _ *http.Request) {
			writeRawFrames(w, [][]byte{frame(1, "ok\n"), frame(9, "bad")})
		})
		var got []string
		err := c.Logs(bg, res.ID, containerapi.LogsOptions{}, func(l containerapi.LogLine) error { got = append(got, l.Text); return nil })
		wantRoute(t, err, "Client.Logs->Demux.ContainerAPIFrameInvalid")
		if !reflect.DeepEqual(got, []string{"ok"}) {
			t.Errorf("lines before the damage = %v", got)
		}
	})
	t.Run("a terminal stream that breaks", func(t *testing.T) {
		f, c := engine(t, containerapi.KindDocker)
		res, _ := c.CreateContainer(bg, containerapi.ContainerSpec{Name: "p", Image: containerapitest.ImageBusyBox, Tty: true})
		f.Override(http.MethodGet, "/logs", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Length", "100")
			_, _ = w.Write([]byte("partial line\nmore"))
		})
		var got []string
		err := c.Logs(bg, res.ID, containerapi.LogsOptions{}, func(l containerapi.LogLine) error { got = append(got, l.Text); return nil })
		wantRoute(t, err, "Client.Logs.ContainerAPIStreamFailed")
		if len(got) != 1 || got[0] != "partial line" {
			t.Errorf("lines = %v", got)
		}
	})
	t.Run("the engine refuses", func(t *testing.T) {
		f, c := engine(t, containerapi.KindDocker)
		res, _ := c.CreateContainer(bg, containerapi.ContainerSpec{Name: "p", Image: containerapitest.ImageBusyBox})
		f.FailNext(http.MethodGet, "/logs", http.StatusInternalServerError, "logging driver does not support reading")
		appErr := routeOf(t, c.Logs(bg, res.ID, containerapi.LogsOptions{}, nil), "Client.Logs->Client.do.ContainerAPIEngineError")
		if !strings.Contains(appErr.Message, "does not support reading") {
			t.Errorf("message = %q", appErr.Message)
		}
	})
}

func TestExec(t *testing.T) {
	for _, kind := range kinds {
		t.Run(string(kind), func(t *testing.T) {
			f, c := engine(t, kind)
			res, err := c.CreateContainer(bg, containerapi.ContainerSpec{Name: "x", Image: containerapitest.ImageBusyBox, Cmd: []string{"sleep", "9"}})
			if err != nil {
				t.Fatal(err)
			}
			// not running: the engine refuses
			_, err = c.Exec(bg, res.ID, containerapi.ExecOptions{Cmd: []string{"true"}})
			wantRoute(t, err, "Client.Exec->Client.call->Client.do.ContainerAPIConflict")
			if err := c.StartContainer(bg, res.ID); err != nil {
				t.Fatal(err)
			}
			for _, tc := range []struct {
				name   string
				opts   containerapi.ExecOptions
				stdout string
				stderr string
				code   int
			}{
				{"both outputs and a status", containerapi.ExecOptions{Cmd: []string{"sh", "-c", "echo hi; echo bad >&2; exit 3"}}, "hi\n", "bad\n", 3},
				{"a command that works", containerapi.ExecOptions{Cmd: []string{"echo", "a", "b"}}, "a b\n", "", 0},
				{"a command that does not exist", containerapi.ExecOptions{Cmd: []string{"nonesuch"}}, "", "nonesuch: not found\n", 127},
				{"no output at all", containerapi.ExecOptions{Cmd: []string{"true"}, Env: []string{"A=1"}, WorkingDir: "/tmp", User: "me"}, "", "", 0},
			} {
				r, err := c.Exec(bg, res.ID, tc.opts)
				if err != nil {
					t.Fatalf("%s: %v", tc.name, err)
				}
				if r.Stdout != tc.stdout || r.Stderr != tc.stderr || r.ExitCode != tc.code || r.Running || r.Truncated {
					t.Errorf("%s: result = %+v", tc.name, r)
				}
			}
			var body map[string]any
			if err := jsonUnmarshal(lastRequest(t, f, http.MethodPost, "/exec").Body, &body); err != nil {
				t.Fatal(err)
			}
			if body["WorkingDir"] != "/tmp" || body["User"] != "me" || !reflect.DeepEqual(body["Env"], []any{"A=1"}) || body["AttachStdout"] != true || body["Tty"] != false {
				t.Errorf("exec create body = %v", body)
			}
		})
	}
}

func TestExecLimitsAndWaits(t *testing.T) {
	t.Run("output beyond the limit is dropped", func(t *testing.T) {
		f, c := engine(t, containerapi.KindDocker)
		res, _ := c.CreateContainer(bg, containerapi.ContainerSpec{Name: "x", Image: containerapitest.ImageBusyBox})
		_ = c.StartContainer(bg, res.ID)
		f.SetExec(func([]string) (string, string, int) { return strings.Repeat("x", 1000), strings.Repeat("e", 1000), 0 })
		r, err := c.Exec(bg, res.ID, containerapi.ExecOptions{Cmd: []string{"big"}, MaxOutput: 1500})
		if err != nil {
			t.Fatal(err)
		}
		if !r.Truncated || len(r.Stdout)+len(r.Stderr) != 1500 {
			t.Errorf("result: truncated %v, %d + %d bytes", r.Truncated, len(r.Stdout), len(r.Stderr))
		}
	})
	t.Run("a command that has not ended is reported as running", func(t *testing.T) {
		f, c := engine(t, containerapi.KindDocker)
		res, _ := c.CreateContainer(bg, containerapi.ContainerSpec{Name: "x", Image: containerapitest.ImageBusyBox})
		_ = c.StartContainer(bg, res.ID)
		f.Override(http.MethodGet, "/json", func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/v") && strings.Contains(r.URL.Path, "/exec/") {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"Running":true,"ExitCode":0}`))
				return
			}
			http.NotFound(w, r)
		})
		r, err := c.Exec(bg, res.ID, containerapi.ExecOptions{Cmd: []string{"sleep"}, Wait: 100 * time.Millisecond})
		if err != nil {
			t.Fatal(err)
		}
		if !r.Running || r.ExitCode != -1 {
			t.Errorf("result = %+v", r)
		}
	})
	t.Run("a cancelled wait is an error", func(t *testing.T) {
		f, c := engine(t, containerapi.KindDocker)
		res, _ := c.CreateContainer(bg, containerapi.ContainerSpec{Name: "x", Image: containerapitest.ImageBusyBox})
		_ = c.StartContainer(bg, res.ID)
		// The wait is cancelled once it has begun (the first look at the exec), not after a fixed time: a timer could
		// fire while the exec is still being created on a loaded machine, and that is a failed request, not a wait.
		ctx, cancel := context.WithCancel(bg)
		defer cancel()
		f.Override(http.MethodGet, "/json", func(w http.ResponseWriter, r *http.Request) {
			if strings.Contains(r.URL.Path, "/exec/") {
				_, _ = w.Write([]byte(`{"Running":true}`))
				cancel()
				return
			}
			http.NotFound(w, r)
		})
		_, err := c.Exec(ctx, res.ID, containerapi.ExecOptions{Cmd: []string{"sleep"}, Wait: time.Minute})
		wantRoute(t, err, "Client.Exec.ContainerAPIExecCancelled")
		if !errors.Is(err, context.Canceled) {
			t.Errorf("the context error is lost: %v", err)
		}
	})
	t.Run("a damaged output stream", func(t *testing.T) {
		f, c := engine(t, containerapi.KindDocker)
		res, _ := c.CreateContainer(bg, containerapi.ContainerSpec{Name: "x", Image: containerapitest.ImageBusyBox})
		_ = c.StartContainer(bg, res.ID)
		f.Override(http.MethodPost, "/start", func(w http.ResponseWriter, r *http.Request) {
			if strings.Contains(r.URL.Path, "/exec/") {
				writeRawFrames(w, [][]byte{frame(7, "junk")})
				return
			}
			http.NotFound(w, r)
		})
		_, err := c.Exec(bg, res.ID, containerapi.ExecOptions{Cmd: []string{"x"}})
		wantRoute(t, err, "Client.Exec->Demux.ContainerAPIFrameInvalid")
	})
	t.Run("the engine refuses the start", func(t *testing.T) {
		f, c := engine(t, containerapi.KindDocker)
		res, _ := c.CreateContainer(bg, containerapi.ContainerSpec{Name: "x", Image: containerapitest.ImageBusyBox})
		_ = c.StartContainer(bg, res.ID)
		f.FailNext(http.MethodPost, "/start", http.StatusInternalServerError, "OCI runtime exec failed")
		_, err := c.Exec(bg, res.ID, containerapi.ExecOptions{Cmd: []string{"x"}})
		wantRoute(t, err, "Client.Exec->Client.do.ContainerAPIEngineError")
	})
	t.Run("the exit status cannot be read", func(t *testing.T) {
		f, c := engine(t, containerapi.KindDocker)
		res, _ := c.CreateContainer(bg, containerapi.ContainerSpec{Name: "x", Image: containerapitest.ImageBusyBox})
		_ = c.StartContainer(bg, res.ID)
		f.FailNext(http.MethodGet, "/json", http.StatusInternalServerError, "no exec state")
		// FailNext hits the first /json: the inspect of the container is done by Exec? no, it is the exec's own
		_, err := c.Exec(bg, res.ID, containerapi.ExecOptions{Cmd: []string{"true"}})
		wantRoute(t, err, "Client.Exec->Client.call->Client.do.ContainerAPIEngineError")
	})
}
