package claude

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestParseLine(t *testing.T) {
	tests := []struct {
		name  string
		line  string
		check func(t *testing.T, m Message)
	}{
		{"system init", `{"type":"system","subtype":"init","uuid":"u1","session_id":"s1","cwd":"/w","model":"m1","permissionMode":"plan","tools":["Read","Bash"],"slash_commands":["compact"],"skills":["x"],"apiKeySource":"none","output_style":"default","mcp_servers":[{"name":"docs","status":"connected"}],"capabilities":["interrupt_receipt_v1","other"],"claude_code_version":"9.9.9","agents":["a"]}`,
			func(t *testing.T, m Message) {
				in := m.(*InitMessage)
				want := &InitMessage{UUID: "u1", SessionID: "s1", CWD: "/w", Model: "m1", PermissionMode: "plan", Tools: []string{"Read", "Bash"},
					SlashCommands: []string{"compact"}, Skills: []string{"x"}, APIKeySource: "none", OutputStyle: "default",
					MCPServers: []MCPServer{{"docs", "connected"}}, Capabilities: Capabilities{"interrupt_receipt_v1", "other"}, ClaudeCodeVersion: "9.9.9"}
				in.raw = raw{}
				if !reflect.DeepEqual(in, want) {
					t.Errorf("init = %+v, want %+v", in, want)
				}
				if !want.Capabilities.Has(CapInterruptReceipt) || want.Capabilities.Has(CapMCPReadResource) {
					t.Error("Capabilities.Has")
				}
			}},
		{"system status", `{"type":"system","subtype":"status","uuid":"u2","session_id":"s1","status":"compacting"}`,
			func(t *testing.T, m Message) {
				s := m.(*SystemMessage)
				if s.Subtype != "status" || s.UUID != "u2" || s.SessionID != "s1" {
					t.Errorf("system = %+v", s)
				}
			}},
		{"assistant with text, thinking and a tool call", `{"type":"assistant","uuid":"a1","session_id":"s1","parent_tool_use_id":"toolu_p","message":{"id":"msg_1","model":"m1","stop_reason":"tool_use","content":[{"type":"thinking","thinking":"hmm","signature":"sig"},{"type":"text","text":"Hello "},{"type":"text","text":"there"},{"type":"tool_use","id":"toolu_1","name":"Read","input":{"file_path":"/w/a.go"}},{"type":"mystery","x":1}]},"error":"rate_limit"}`,
			func(t *testing.T, m Message) {
				a := m.(*AssistantMessage)
				if a.UUID != "a1" || a.SessionID != "s1" || a.ParentToolUseID != "toolu_p" || a.MessageID != "msg_1" || a.Model != "m1" || a.StopReason != "tool_use" || a.Error != "rate_limit" {
					t.Errorf("assistant = %+v", a)
				}
				if len(a.Blocks) != 5 {
					t.Fatalf("blocks = %d", len(a.Blocks))
				}
				if b := a.Blocks[0]; b.Type != "thinking" || b.Thinking != "hmm" || b.Signature != "sig" {
					t.Errorf("thinking block = %+v", b)
				}
				if b := a.Blocks[3]; b.Type != "tool_use" || b.ID != "toolu_1" || b.Name != "Read" || string(b.Input) != `{"file_path":"/w/a.go"}` {
					t.Errorf("tool_use block = %+v", b)
				}
				if b := a.Blocks[4]; b.Type != "mystery" || string(b.Raw) != `{"type":"mystery","x":1}` {
					t.Errorf("unknown block = %+v", b)
				}
				if a.Text() != "Hello there" {
					t.Errorf("Text = %q", a.Text())
				}
			}},
		{"user with a plain string", `{"type":"user","uuid":"u3","session_id":"s1","parent_tool_use_id":null,"message":{"role":"user","content":"fix the bug"},"isReplay":true}`,
			func(t *testing.T, m Message) {
				u := m.(*UserMessage)
				if u.UUID != "u3" || !u.IsReplay || u.IsSynthetic || u.ParentToolUseID != "" || u.Text() != "fix the bug" {
					t.Errorf("user = %+v", u)
				}
			}},
		{"user with a tool result as a string", `{"type":"user","uuid":"u4","session_id":"s1","parent_tool_use_id":null,"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"package main","is_error":false}]},"tool_use_result":{"type":"text"},"isSynthetic":true}`,
			func(t *testing.T, m Message) {
				u := m.(*UserMessage)
				b := u.Blocks[0]
				if b.Type != "tool_result" || b.ToolUseID != "toolu_1" || b.ResultText() != "package main" || b.IsError || !u.IsSynthetic || string(u.ToolUseResult) != `{"type":"text"}` {
					t.Errorf("user = %+v", u)
				}
			}},
		{"user with a tool result as blocks and an error", `{"type":"user","uuid":"u5","session_id":"s1","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_2","is_error":true,"content":[{"type":"text","text":"line 1"},{"type":"image","source":{}},{"type":"text","text":"line 2"}]}]}}`,
			func(t *testing.T, m Message) {
				b := m.(*UserMessage).Blocks[0]
				if !b.IsError || b.ResultText() != "line 1\nline 2" {
					t.Errorf("block = %+v, text %q", b, b.ResultText())
				}
			}},
		{"stream message_start", `{"type":"stream_event","uuid":"e1","session_id":"s1","parent_tool_use_id":null,"event":{"type":"message_start","message":{"id":"msg_1","model":"m1"}}}`,
			func(t *testing.T, m Message) {
				e := m.(*StreamEvent)
				if e.EventType != "message_start" || e.MessageID != "msg_1" || e.Model != "m1" || e.UUID != "e1" {
					t.Errorf("event = %+v", e)
				}
			}},
		{"stream content_block_start of a tool call", `{"type":"stream_event","uuid":"e2","session_id":"s1","parent_tool_use_id":"toolu_p","event":{"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"toolu_9","name":"Bash","input":{}}}}`,
			func(t *testing.T, m Message) {
				e := m.(*StreamEvent)
				if e.Index != 2 || e.BlockType != "tool_use" || e.BlockID != "toolu_9" || e.BlockName != "Bash" || e.ParentToolUseID != "toolu_p" {
					t.Errorf("event = %+v", e)
				}
			}},
		{"stream text delta", `{"type":"stream_event","uuid":"e3","session_id":"s1","event":{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"Hel"}}}`,
			func(t *testing.T, m Message) {
				e := m.(*StreamEvent)
				if e.DeltaType != "text_delta" || e.Text != "Hel" || e.Index != 1 {
					t.Errorf("event = %+v", e)
				}
			}},
		{"stream thinking delta", `{"type":"stream_event","uuid":"e4","session_id":"s1","event":{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"hm"}}}`,
			func(t *testing.T, m Message) {
				if e := m.(*StreamEvent); e.DeltaType != "thinking_delta" || e.Thinking != "hm" {
					t.Errorf("event = %+v", e)
				}
			}},
		{"stream input json delta", `{"type":"stream_event","uuid":"e5","session_id":"s1","event":{"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"{\"a\":"}}}`,
			func(t *testing.T, m Message) {
				if e := m.(*StreamEvent); e.DeltaType != "input_json_delta" || e.PartialJSON != `{"a":` {
					t.Errorf("event = %+v", e)
				}
			}},
		{"stream message_delta", `{"type":"stream_event","uuid":"e6","session_id":"s1","event":{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":3}}}`,
			func(t *testing.T, m Message) {
				if e := m.(*StreamEvent); e.EventType != "message_delta" || e.StopReason != "end_turn" {
					t.Errorf("event = %+v", e)
				}
			}},
		{"result of a success", `{"type":"result","subtype":"success","uuid":"r1","session_id":"s1","is_error":false,"duration_ms":1500,"duration_api_ms":1200,"num_turns":3,"result":"Done.","stop_reason":"end_turn","total_cost_usd":0.42,"usage":{"input_tokens":1},"modelUsage":{"m1":{}},"permission_denials":[{"tool_name":"Bash"}],"terminal_reason":"completed"}`,
			func(t *testing.T, m Message) {
				r := m.(*ResultMessage)
				if r.Subtype != "success" || r.IsError || r.NumTurns != 3 || r.DurationMS != 1500 || r.DurationAPIMS != 1200 || r.TotalCostUSD != 0.42 ||
					r.Result != "Done." || r.StopReason != "end_turn" || r.TerminalReason != "completed" || r.SessionID != "s1" {
					t.Errorf("result = %+v", r)
				}
				if string(r.Usage) != `{"input_tokens":1}` || string(r.PermissionDenials) != `[{"tool_name":"Bash"}]` || string(r.ModelUsage) != `{"m1":{}}` {
					t.Errorf("raw parts = %s %s %s", r.Usage, r.PermissionDenials, r.ModelUsage)
				}
			}},
		{"result of an error", `{"type":"result","subtype":"error_max_turns","uuid":"r2","session_id":"s1","is_error":true,"duration_ms":10,"duration_api_ms":9,"num_turns":50,"errors":["too many turns"],"total_cost_usd":1}`,
			func(t *testing.T, m Message) {
				r := m.(*ResultMessage)
				if r.Subtype != "error_max_turns" || !r.IsError || !reflect.DeepEqual(r.Errors, []string{"too many turns"}) || r.NumTurns != 50 {
					t.Errorf("result = %+v", r)
				}
			}},
		{"an unknown type is kept as it is", `{"type":"rate_limit_event","uuid":"x","session_id":"s1","rate_limit_info":{"status":"allowed"}}`,
			func(t *testing.T, m Message) {
				u := m.(*UnknownMessage)
				if u.Type != "rate_limit_event" || string(u.RawJSON()) != `{"type":"rate_limit_event","uuid":"x","session_id":"s1","rate_limit_info":{"status":"allowed"}}` {
					t.Errorf("unknown = %+v", u)
				}
			}},
		{"a line with no type is unknown too", `{"hello":"world"}`,
			func(t *testing.T, m Message) {
				if u := m.(*UnknownMessage); u.Type != "" || string(u.RawJSON()) != `{"hello":"world"}` {
					t.Errorf("unknown = %+v", u)
				}
			}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := parseLine([]byte(tt.line))
			if err != nil {
				t.Fatal(err)
			}
			if string(m.RawJSON()) != tt.line {
				t.Errorf("RawJSON = %s", m.RawJSON())
			}
			tt.check(t, m)
		})
	}
}

func TestParseLineErrors(t *testing.T) {
	tests := []struct {
		name  string
		line  string
		route string
	}{
		{"not JSON", `{broken`, "parseLine->parseEnvelope.MalformedLine"},
		{"a JSON array", `[1,2]`, "parseLine->parseEnvelope.MalformedLine"},
		{"assistant whose message is a string", `{"type":"assistant","message":"oops"}`, "parseLine->buildMessage->parseAssistant->decode.MessageInvalid"},
		{"assistant whose content is a number", `{"type":"assistant","message":{"content":5}}`, "buildMessage->parseAssistant->blocksOf->decode.MessageInvalid"},
		{"user whose content has a block that is not an object", `{"type":"user","message":{"content":[1]}}`, "buildMessage->parseUser->blocksOf->decode.MessageInvalid"},
		{"result with a count that is not a number", `{"type":"result","num_turns":"many"}`, "buildMessage->parseResult->decode.MessageInvalid"},
		{"init with tools that are not a list", `{"type":"system","subtype":"init","tools":"x"}`, "buildMessage->parseInit->decode.MessageInvalid"},
		{"stream event whose event is a string", `{"type":"stream_event","event":"x"}`, "buildMessage->parseStream->decode.MessageInvalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseLine([]byte(tt.line))
			wantRoute(t, err, tt.route)
		})
	}
}

func TestPermissionUpdateKeepsWhatItDoesNotKnow(t *testing.T) {
	const in = `{"type":"addRules","rules":[{"toolName":"Bash","ruleContent":"npm test:*"}],"behavior":"allow","destination":"localSettings","future":{"a":1}}`
	var u PermissionUpdate
	if err := json.Unmarshal([]byte(in), &u); err != nil {
		t.Fatal(err)
	}
	if u.Type != "addRules" || u.Behavior != "allow" || u.Destination != "localSettings" || u.Rules[0] != (PermissionRule{"Bash", "npm test:*"}) {
		t.Errorf("update = %+v", u)
	}
	out, err := json.Marshal(u)
	if err != nil || string(out) != in {
		t.Errorf("written back as %s, %v", out, err)
	}
	session := u.WithDestination("session")
	out, err = json.Marshal(session)
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"type":"addRules","rules":[{"toolName":"Bash","ruleContent":"npm test:*"}],"behavior":"allow","destination":"session"}`; string(out) != want {
		t.Errorf("with another destination = %s, want %s", out, want)
	}
	if u.Destination != "localSettings" {
		t.Error("WithDestination changed the original")
	}
	var bad PermissionUpdate
	if err := json.Unmarshal([]byte(`[1]`), &bad); err == nil {
		t.Error("an update that is not an object was accepted")
	}
}

func TestDecisionBody(t *testing.T) {
	req := ToolRequest{ToolName: "Edit", Input: json.RawMessage(`{"file_path":"/w/a.go"}`)}
	sugg := PermissionUpdate{Type: "setMode", Mode: "acceptEdits", Destination: "session"}
	tests := []struct {
		name string
		req  ToolRequest
		d    Decision
		want string
	}{
		{"allow keeps the input", req, Decision{Allow: true}, `{"behavior":"allow","updatedInput":{"file_path":"/w/a.go"}}`},
		{"allow with the input the user changed", req, Decision{Allow: true, UpdatedInput: json.RawMessage(`{"file_path":"/w/a.go","new_string":"x"}`)},
			`{"behavior":"allow","updatedInput":{"file_path":"/w/a.go","new_string":"x"}}`},
		{"allow with a rule change", req, Decision{Allow: true, UpdatedPermissions: []PermissionUpdate{sugg}},
			`{"behavior":"allow","updatedInput":{"file_path":"/w/a.go"},"updatedPermissions":[{"type":"setMode","mode":"acceptEdits","destination":"session"}]}`},
		{"allow with no input at all", ToolRequest{}, Decision{Allow: true}, `{"behavior":"allow","updatedInput":{}}`},
		{"deny", req, Decision{Message: "not now"}, `{"behavior":"deny","message":"not now"}`},
		{"deny and stop", req, Decision{Message: "stop", Interrupt: true}, `{"behavior":"deny","message":"stop","interrupt":true}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := json.Marshal(decisionBody(tt.req, tt.d))
			if err != nil || string(got) != tt.want {
				t.Errorf("body = %s, %v; want %s", got, err, tt.want)
			}
		})
	}
}

func TestParseToolRequest(t *testing.T) {
	tests := []struct {
		name  string
		line  string
		ok    bool
		check func(t *testing.T, r ToolRequest)
		route string
	}{
		{"a request", `{"type":"control_request","request_id":"p1","request":{"subtype":"can_use_tool","tool_name":"Bash","input":{"command":"ls"},"tool_use_id":"t1","title":"Run ls?","permission_suggestions":[{"type":"addRules","destination":"localSettings","rules":[{"toolName":"Bash"}],"behavior":"allow"}]}}`, true,
			func(t *testing.T, r ToolRequest) {
				if r.RequestID != "p1" || r.ToolName != "Bash" || r.ToolUseID != "t1" || r.Title != "Run ls?" || string(r.Input) != `{"command":"ls"}` || len(r.Suggestions) != 1 {
					t.Errorf("request = %+v", r)
				}
			}, ""},
		{"another control request", `{"type":"control_request","request_id":"p1","request":{"subtype":"hook_callback"}}`, false, nil, ""},
		{"a message", `{"type":"assistant","message":{"content":[]}}`, false, nil, ""},
		{"not JSON", `{broken`, false, nil, "ParseToolRequest->parseEnvelope.MalformedLine"},
		{"a request that is not an object", `{"type":"control_request","request_id":"p1","request":"x"}`, false, nil, "ParseToolRequest->decode.MessageInvalid"},
		{"a request with a tool that is a number", `{"type":"control_request","request_id":"p1","request":{"subtype":"can_use_tool","tool_name":5}}`, false, nil, "ParseToolRequest->decode.MessageInvalid"},
		{"a request without a tool", `{"type":"control_request","request_id":"p1","request":{"subtype":"can_use_tool"}}`, false, nil, "ParseToolRequest.ToolRequestInvalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, ok, err := ParseToolRequest([]byte(tt.line))
			if tt.route != "" {
				wantRoute(t, err, tt.route)
				return
			}
			if err != nil || ok != tt.ok {
				t.Fatalf("ok %v err %v, want ok %v", ok, err, tt.ok)
			}
			if tt.check != nil {
				tt.check(t, req)
			}
		})
	}
}
