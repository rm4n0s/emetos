package dap

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestFormatMessage(t *testing.T) {
	tests := []struct {
		name string
		m    Message
		want string
	}{
		{"no placeholder", Message{Format: "plain"}, "plain"},
		{"a variable", Message{Format: "cannot open {path}", Variables: map[string]string{"path": "/a/b"}}, "cannot open /a/b"},
		{"two variables and one repeated", Message{Format: "{a}-{b}-{a}", Variables: map[string]string{"a": "1", "b": "2"}}, "1-2-1"},
		{"a placeholder with no variable stays", Message{Format: "x {missing} y", Variables: map[string]string{"a": "1"}}, "x {missing} y"},
		{"no variables at all", Message{Format: "x {a}"}, "x {a}"},
		{"an empty value", Message{Format: "[{a}]", Variables: map[string]string{"a": ""}}, "[]"},
		{"a lone brace", Message{Format: "} {", Variables: map[string]string{}}, "} {"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatMessage(tt.m); got != tt.want {
				t.Errorf("FormatMessage = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestModuleID(t *testing.T) {
	tests := []struct {
		name, in, want string
		out            string
	}{
		{"a number", `7`, "7", `7`},
		{"a string", `"abc"`, "abc", `"abc"`},
		{"a numeric string comes back as a number", `"12"`, "12", `12`},
		{"a big number", `123456789012`, "123456789012", `123456789012`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var m ModuleID
			if err := json.Unmarshal([]byte(tt.in), &m); err != nil {
				t.Fatal(err)
			}
			if string(m) != tt.want {
				t.Errorf("id = %q, want %q", m, tt.want)
			}
			out, err := json.Marshal(m)
			if err != nil || string(out) != tt.out {
				t.Errorf("marshal = %s, %v, want %s", out, err, tt.out)
			}
		})
	}
	var m ModuleID
	if err := json.Unmarshal([]byte(`{"a":1}`), &m); err == nil {
		t.Error("an object is not a module id")
	}
	if err := json.Unmarshal([]byte(`"unterminated`), &m); err == nil {
		t.Error("broken JSON is not a module id")
	}
}

func TestParseEvent(t *testing.T) {
	tests := []struct {
		name  string
		event string
		body  string
		want  any
	}{
		{"initialized has no body", EventInitialized, ``, &struct{}{}},
		{"stopped", EventStopped, `{"reason":"breakpoint","threadId":3,"allThreadsStopped":true,"hitBreakpointIds":[1,2],"text":"t","description":"d","preserveFocusHint":true}`,
			&StoppedEventBody{Reason: "breakpoint", ThreadID: 3, AllThreadsStopped: true, HitBreakpointIDs: []int{1, 2}, Text: "t", Description: "d", PreserveFocusHint: true}},
		{"continued", EventContinued, `{"threadId":4,"allThreadsContinued":true}`, &ContinuedEventBody{ThreadID: 4, AllThreadsContinued: true}},
		{"exited", EventExited, `{"exitCode":2}`, &ExitedEventBody{ExitCode: 2}},
		{"terminated without a body", EventTerminated, `null`, &TerminatedEventBody{}},
		{"terminated with restart data", EventTerminated, `{"restart":{"k":1}}`, &TerminatedEventBody{Restart: json.RawMessage(`{"k":1}`)}},
		{"thread", EventThread, `{"reason":"started","threadId":9}`, &ThreadEventBody{Reason: "started", ThreadID: 9}},
		{"output", EventOutput, `{"category":"stderr","output":"x\n","group":"start","variablesReference":5,"source":{"path":"/a.go"},"line":3,"column":4,"data":[1],"locationReference":6}`,
			&OutputEventBody{Category: "stderr", Output: "x\n", Group: "start", VariablesReference: 5, Source: &Source{Path: "/a.go"}, Line: 3, Column: 4, Data: json.RawMessage(`[1]`), LocationReference: 6}},
		{"breakpoint", EventBreakpoint, `{"reason":"changed","breakpoint":{"id":1,"verified":true,"line":7,"message":"m"}}`,
			&BreakpointEventBody{Reason: "changed", Breakpoint: Breakpoint{ID: 1, Verified: true, Line: 7, Message: "m"}}},
		{"module", EventModule, `{"reason":"new","module":{"id":"m1","name":"mod","path":"/p","isUserCode":true}}`,
			&ModuleEventBody{Reason: "new", Module: Module{ID: "m1", Name: "mod", Path: "/p", IsUserCode: true}}},
		{"loadedSource", EventLoadedSource, `{"reason":"removed","source":{"name":"a","sourceReference":2}}`,
			&LoadedSourceEventBody{Reason: "removed", Source: Source{Name: "a", SourceReference: 2}}},
		{"process", EventProcess, `{"name":"prog","systemProcessId":42,"isLocalProcess":true,"startMethod":"launch","pointerSize":64}`,
			&ProcessEventBody{Name: "prog", SystemProcessID: 42, IsLocalProcess: true, StartMethod: "launch", PointerSize: 64}},
		{"capabilities", EventCapabilities, `{"capabilities":{"supportsStepBack":true}}`, &CapabilitiesEventBody{Capabilities: Capabilities{SupportsStepBack: true}}},
		{"progressStart", EventProgressStart, `{"progressId":"p","title":"T","requestId":3,"cancellable":true,"message":"m","percentage":12.5}`,
			&ProgressStartEventBody{ProgressID: "p", Title: "T", RequestID: 3, Cancellable: true, Message: "m", Percentage: ptr(12.5)}},
		{"progressUpdate", EventProgressUpdate, `{"progressId":"p","message":"m","percentage":50}`, &ProgressUpdateEventBody{ProgressID: "p", Message: "m", Percentage: ptr(50.0)}},
		{"progressEnd", EventProgressEnd, `{"progressId":"p","message":"done"}`, &ProgressEndEventBody{ProgressID: "p", Message: "done"}},
		{"invalidated", EventInvalidated, `{"areas":["stacks","variables"],"threadId":1,"stackFrameId":2}`, &InvalidatedEventBody{Areas: []string{"stacks", "variables"}, ThreadID: 1, StackFrameID: 2}},
		{"memory", EventMemory, `{"memoryReference":"0x10","offset":4,"count":8}`, &MemoryEventBody{MemoryReference: "0x10", Offset: 4, Count: 8}},
		{"an event of the adapter's own", "dlvSomething", `{"x":1}`, json.RawMessage(`{"x":1}`)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseEvent(Event{Event: tt.event, Body: json.RawMessage(tt.body)})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseEvent = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func ptr[T any](v T) *T { return &v }

func TestParseEventBodyInvalid(t *testing.T) {
	_, err := ParseEvent(Event{Event: EventStopped, Body: json.RawMessage(`{"reason":5}`)})
	appErr := wantRouteError(t, err, "ParseEvent.DapBodyInvalid")
	if appErr.Metadata["event"] != EventStopped {
		t.Errorf("event = %v", appErr.Metadata["event"])
	}
}

// TestRequestJSON pins the JSON of the requests the session sends, against the protocol's schema: names, omitted members and
// the members that must be sent even when false or zero.
func TestRequestJSON(t *testing.T) {
	yes, no := true, false
	tests := []struct {
		name string
		v    any
		want string
	}{
		{"initialize", InitializeRequestArguments{ClientID: "emetos", ClientName: "Emetos", AdapterID: "go", LinesStartAt1: true, ColumnsStartAt1: true, PathFormat: "path",
			SupportsVariableType: true, SupportsVariablePaging: true, SupportsRunInTerminalRequest: true, SupportsStartDebuggingRequest: true, SupportsProgressReporting: true, SupportsInvalidatedEvent: true},
			`{"clientID":"emetos","clientName":"Emetos","adapterID":"go","linesStartAt1":true,"columnsStartAt1":true,"pathFormat":"path","supportsVariableType":true,"supportsVariablePaging":true,"supportsRunInTerminalRequest":true,"supportsProgressReporting":true,"supportsInvalidatedEvent":true,"supportsStartDebuggingRequest":true}`},
		{"initialize with the lines starting at 0", InitializeRequestArguments{AdapterID: "x"}, `{"adapterID":"x","linesStartAt1":false,"columnsStartAt1":false}`},
		{"setBreakpoints with none clears the file", SetBreakpointsArguments{Source: Source{Path: "/a.go"}, Breakpoints: []SourceBreakpoint{}}, `{"source":{"path":"/a.go"},"breakpoints":[]}`},
		{"setBreakpoints", SetBreakpointsArguments{Source: Source{Path: "/a.go"}, Breakpoints: []SourceBreakpoint{{Line: 5}, {Line: 9, Column: 3, Condition: "i == 2", HitCondition: "3", LogMessage: "x={x}"}}},
			`{"source":{"path":"/a.go"},"breakpoints":[{"line":5},{"line":9,"column":3,"condition":"i == 2","hitCondition":"3","logMessage":"x={x}"}]}`},
		{"setExceptionBreakpoints", SetExceptionBreakpointsArguments{Filters: []string{"raised"}, FilterOptions: []ExceptionFilterOptions{{FilterID: "uncaught", Condition: "c"}}},
			`{"filters":["raised"],"filterOptions":[{"filterId":"uncaught","condition":"c"}]}`},
		{"disconnect that terminates", DisconnectArguments{TerminateDebuggee: &yes}, `{"terminateDebuggee":true}`},
		{"disconnect that does not terminate", DisconnectArguments{TerminateDebuggee: &no, Restart: true}, `{"restart":true,"terminateDebuggee":false}`},
		{"disconnect that says nothing", DisconnectArguments{}, `{}`},
		{"continue", ContinueArguments{ThreadID: 0}, `{"threadId":0}`},
		{"next", StepArguments{ThreadID: 1, Granularity: GranularityInstruction}, `{"threadId":1,"granularity":"instruction"}`},
		{"stackTrace", StackTraceArguments{ThreadID: 2, Levels: 20, Format: &StackFrameFormat{Parameters: true}}, `{"threadId":2,"levels":20,"format":{"parameters":true}}`},
		{"variables paged", VariablesArguments{VariablesReference: 7, Filter: "indexed", Start: 100, Count: 100}, `{"variablesReference":7,"filter":"indexed","start":100,"count":100}`},
		{"evaluate for a hover", EvaluateArguments{Expression: "a.b", FrameID: 3, Context: ContextHover}, `{"expression":"a.b","frameId":3,"context":"hover"}`},
		{"runInTerminal arguments with a removed variable", RunInTerminalRequestArguments{Cwd: "/w", Args: []string{"go", "run"}, Env: map[string]*string{"A": ptr("1"), "B": nil}},
			`{"cwd":"/w","args":["go","run"],"env":{"A":"1","B":null}}`},
		{"cancel", CancelArguments{RequestID: 12}, `{"requestId":12}`},
		{"dataBreakpointInfo", DataBreakpointInfoArguments{VariablesReference: 1, Name: "x"}, `{"variablesReference":1,"name":"x"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := json.Marshal(tt.v)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tt.want {
				t.Errorf("JSON =\n%s\nwant\n%s", got, tt.want)
			}
		})
	}
}

// TestResponseJSON reads the bodies of a real adapter's answers (the shapes dlv, debugpy and gdb send).
func TestResponseJSON(t *testing.T) {
	var caps Capabilities
	in := `{"supportsConfigurationDoneRequest":true,"supportsFunctionBreakpoints":true,"supportsConditionalBreakpoints":true,
	"supportsSetVariable":true,"exceptionBreakpointFilters":[{"filter":"raised","label":"Raised","default":true,"supportsCondition":true}],
	"supportedChecksumAlgorithms":["MD5"],"breakpointModes":[{"mode":"m","label":"M","appliesTo":["source"]}],"supportsANSIStyling":true}`
	if err := json.Unmarshal([]byte(in), &caps); err != nil {
		t.Fatal(err)
	}
	if !caps.SupportsConfigurationDoneRequest || !caps.SupportsSetVariable || !caps.SupportsANSIStyling || caps.SupportsStepBack {
		t.Errorf("capabilities = %+v", caps)
	}
	if len(caps.ExceptionBreakpointFilters) != 1 || !caps.ExceptionBreakpointFilters[0].Default || caps.BreakpointModes[0].AppliesTo[0] != "source" {
		t.Errorf("capabilities = %+v", caps)
	}

	var st StackTraceResponseBody
	body := `{"stackFrames":[{"id":1000,"name":"main.main","source":{"name":"main.go","path":"/p/main.go"},"line":12,"column":2,"moduleId":3,"presentationHint":"subtle"},
	{"id":1001,"name":"runtime.main","line":1,"column":0,"canRestart":true}],"totalFrames":2}`
	if err := json.Unmarshal([]byte(body), &st); err != nil {
		t.Fatal(err)
	}
	if st.StackFrames[0].Source.Path != "/p/main.go" || st.StackFrames[0].ModuleID != "3" || st.StackFrames[1].Source != nil || !st.StackFrames[1].CanRestart || st.TotalFrames != 2 {
		t.Errorf("stack = %+v", st)
	}

	var cont ContinueResponseBody
	if err := json.Unmarshal([]byte(`{}`), &cont); err != nil || cont.AllThreadsContinued != nil {
		t.Errorf("an absent allThreadsContinued must stay nil (the protocol says true): %+v %v", cont, err)
	}
	var di DataBreakpointInfoResponseBody
	if err := json.Unmarshal([]byte(`{"dataId":null,"description":"no"}`), &di); err != nil || di.DataID != nil {
		t.Errorf("a null dataId must be nil: %+v %v", di, err)
	}
}

func TestDecodeMessage(t *testing.T) {
	good := []struct{ name, in, typ string }{
		{"event", `{"seq":1,"type":"event","event":"initialized"}`, TypeEvent},
		{"response", `{"seq":2,"type":"response","request_seq":1,"success":true,"command":"initialize","body":{}}`, TypeResponse},
		{"request", `{"seq":3,"type":"request","command":"runInTerminal","arguments":{}}`, TypeRequest},
	}
	for _, tt := range good {
		t.Run(tt.name, func(t *testing.T) {
			env, err := decodeMessage([]byte(tt.in))
			if err != nil || env.Type != tt.typ {
				t.Errorf("decodeMessage = %+v, %v", env, err)
			}
		})
	}
	bad := []struct{ name, in, reason string }{
		{"not JSON", `{not json`, "not JSON"},
		{"not an object", `[1]`, "not JSON"},
		{"no type", `{"seq":1}`, "unknown type"},
		{"an unknown type", `{"seq":1,"type":"banana"}`, "unknown type"},
		{"an event with no name", `{"seq":1,"type":"event"}`, "event without name"},
		{"a request with no command", `{"seq":1,"type":"request"}`, "request without command"},
		{"a response with no request_seq", `{"seq":1,"type":"response","success":true}`, "response without request_seq"},
	}
	for _, tt := range bad {
		t.Run(tt.name, func(t *testing.T) {
			_, err := decodeMessage([]byte(tt.in))
			if err == nil {
				t.Fatal("no error")
			}
			appErr := wantRouteError(t, err, "decodeMessage.DapProtocolInvalid")
			if appErr.Metadata["reason"] != tt.reason {
				t.Errorf("reason = %v, want %q", appErr.Metadata["reason"], tt.reason)
			}
		})
	}
}
