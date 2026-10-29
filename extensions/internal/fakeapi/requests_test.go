package fakeapi

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

func TestTheFakeAnswersRequestsWithDoublesAndRecordsThem(t *testing.T) {
	m := manifest()
	m.Privileges = []string{extension.PrivilegeCommandsExecute}
	a := New(t, m, extension.ScopeInfo{Kind: extension.ScopeWorkspace, Folder: t.TempDir()})
	OnRequestValue(a, extension.TopicFilesRead, func(_ context.Context, p extension.FilesPathRequest) (any, error) {
		if p.Path == "../x" {
			return nil, errors.New("ConfinePathInvalid", "no ..", "path", p.Path)
		}
		return extension.FilesReadReply{Path: p.Path, Text: "text of " + p.Path}, nil
	})
	var got []string
	ask := func(topic string, payload any) {
		t.Helper()
		if err := a.Events().Request(context.Background(), topic, payload, func(ev extension.Event, err error) {
			if err != nil {
				got = append(got, "error "+err.Error())
				return
			}
			got = append(got, string(ev.Data))
		}); err != nil {
			t.Fatal(err)
		}
	}
	ask(extension.TopicFilesRead, extension.FilesPathRequest{Path: "a.go"})
	ask(extension.TopicFilesRead, extension.FilesPathRequest{Path: "../x"})
	ask(extension.TopicFilesRead, json.RawMessage(`"not a request"`))
	ask(extension.TopicCommandsList, nil) // no double for it
	if len(got) != 0 {
		t.Fatal("a reply came before Pump")
	}
	a.Pump()
	if len(got) != 4 || got[0] != `{"path":"a.go","text":"text of a.go","size":0,"modTime":"0001-01-01T00:00:00Z"}` ||
		got[1] != "error no .." || got[2][:5] != "error" || got[3] != "error the fake has no double or handler for this topic" {
		t.Errorf("replies %q", got)
	}
	if reqs := a.Requests(); len(reqs) != 4 || reqs[0].Topic != extension.TopicFilesRead || string(reqs[3].Data) != "null" {
		t.Errorf("requests %+v", reqs)
	}
	route(t, a.Events().Request(context.Background(), "bad..topic", nil, nil), "Request->CheckTopic.EventTopicInvalid")
}

func TestTheFakeRoutesRequestsToTheHandlersOfTheExtension(t *testing.T) {
	a := New(t, manifest(), extension.ScopeInfo{Kind: extension.ScopeWorkspace})
	if err := a.Events().Handle("fake.echo", func(_ context.Context, ev extension.Event) (any, error) {
		return map[string]json.RawMessage{"echo": ev.Data}, nil
	}); err != nil {
		t.Fatal(err)
	}
	route(t, a.Events().Handle("fake.echo", nil), "HandleAsync.EventHandlerTaken")
	ev, err := a.Ask(context.Background(), "fake.echo", map[string]int{"n": 1})
	if err != nil || string(ev.Data) != `{"echo":{"n":1}}` {
		t.Errorf("Ask = %s, %v", ev.Data, err)
	}
	_, err = a.Ask(context.Background(), "fake.none", nil)
	route(t, err, "API.AskAs.EventNoHandler")
	// the extension's own request reaches its own handler
	var reply string
	if err := a.Events().Request(context.Background(), "fake.echo", 5, func(ev extension.Event, err error) { reply = string(ev.Data) }); err != nil {
		t.Fatal(err)
	}
	a.Pump()
	if reply != `{"echo":5}` {
		t.Errorf("reply %q", reply)
	}
}

func TestTheFakeRecordsPublishedEventsWithTheirPublisher(t *testing.T) {
	a := New(t, manifest(), extension.ScopeInfo{Kind: extension.ScopeWorkspace})
	var heard []extension.Event
	if _, err := a.Events().Subscribe("**", func(ev extension.Event) { heard = append(heard, ev) }); err != nil {
		t.Fatal(err)
	}
	if err := a.Events().Publish("anything.goes", map[string]int{"n": 2}); err != nil {
		t.Fatal(err)
	}
	route(t, a.Events().Publish("bad..topic", nil), "Publish->CheckTopic.EventTopicInvalid")
	route(t, a.Events().Publish("x.y", func() {}), "Publish.FakeEventValueInvalid")
	if p := a.PublishedEvents(); len(p) != 1 || p[0].Topic != "anything.goes" || string(p[0].Data) != `{"n":2}` {
		t.Errorf("published %+v", p)
	}
	if len(heard) != 1 || heard[0].Publisher != manifest().ID {
		t.Errorf("heard %+v", heard)
	}
	a.Emit("emetos.document.opened", nil)
	if len(heard) != 2 || heard[1].Publisher != extension.CorePublisher {
		t.Errorf("a core event has the publisher %q", heard[1].Publisher)
	}
}
