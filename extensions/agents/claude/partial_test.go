package claude

import "testing"

func ev(parent, typ string, index int, mutate func(*StreamEvent)) *StreamEvent {
	e := &StreamEvent{ParentToolUseID: parent, EventType: typ, Index: index}
	if mutate != nil {
		mutate(e)
	}
	return e
}

func textDelta(parent string, index int, s string) *StreamEvent {
	return ev(parent, "content_block_delta", index, func(e *StreamEvent) { e.DeltaType, e.Text = "text_delta", s })
}

func TestPartialsJoinText(t *testing.T) {
	var p Partials
	p.Apply(ev("", "message_start", 0, nil))
	p.Apply(ev("", "content_block_start", 0, func(e *StreamEvent) { e.BlockType = "thinking" }))
	p.Apply(ev("", "content_block_delta", 0, func(e *StreamEvent) { e.DeltaType, e.Thinking = "thinking_delta", "Let me " }))
	p.Apply(ev("", "content_block_delta", 0, func(e *StreamEvent) { e.DeltaType, e.Thinking = "thinking_delta", "look." }))
	p.Apply(ev("", "content_block_start", 1, func(e *StreamEvent) { e.BlockType = "text" }))
	for _, piece := range []string{"Hel", "lo, ", "world."} {
		p.Apply(textDelta("", 1, piece))
	}
	p.Apply(ev("", "content_block_start", 2, func(e *StreamEvent) { e.BlockType, e.BlockID, e.BlockName = "tool_use", "toolu_1", "Read" }))
	for _, piece := range []string{`{"file_path":`, `"/w/a.go"}`} {
		p.Apply(ev("", "content_block_delta", 2, func(e *StreamEvent) { e.DeltaType, e.PartialJSON = "input_json_delta", piece }))
	}
	p.Apply(ev("", "message_stop", 0, nil))

	if got := p.Text(""); got != "Hello, world." {
		t.Errorf("Text = %q", got)
	}
	blocks := p.Blocks("")
	if len(blocks) != 3 {
		t.Fatalf("blocks = %+v", blocks)
	}
	if b := blocks[0]; b.Type != "thinking" || b.Thinking != "Let me look." {
		t.Errorf("thinking block = %+v", b)
	}
	if b := blocks[2]; b.Type != "tool_use" || b.ToolName != "Read" || b.ToolID != "toolu_1" || b.InputJSON != `{"file_path":"/w/a.go"}` {
		t.Errorf("tool block = %+v", b)
	}

	// The whole message replaces the pieces.
	p.Finish("")
	if len(p.Blocks("")) != 0 || p.Text("") != "" {
		t.Error("Finish left blocks")
	}
}

func TestPartialsParentsAndRestarts(t *testing.T) {
	var p Partials
	p.Apply(textDelta("", 0, "main "))         // a delta whose start was not seen
	p.Apply(textDelta("toolu_sub", 0, "sub ")) // a sub-agent writes at the same time
	p.Apply(textDelta("", 0, "text"))
	if p.Text("") != "main text" || p.Text("toolu_sub") != "sub " {
		t.Errorf("texts = %q and %q", p.Text(""), p.Text("toolu_sub"))
	}
	if b := p.Blocks("")[0]; b.Type != "text" {
		t.Errorf("block type = %q", b.Type)
	}
	// the next message of the main conversation starts again
	p.Apply(ev("", "message_start", 0, nil))
	if p.Text("") != "" || p.Text("toolu_sub") != "sub " {
		t.Errorf("after message_start: %q and %q", p.Text(""), p.Text("toolu_sub"))
	}
	p.Reset()
	if p.Text("toolu_sub") != "" {
		t.Error("Reset left blocks")
	}
	// the zero value and an unknown parent are fine
	var zero Partials
	if zero.Text("x") != "" || len(zero.Blocks("x")) != 0 {
		t.Error("zero value")
	}
	zero.Finish("x")
}
