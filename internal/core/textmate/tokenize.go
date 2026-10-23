package textmate

import (
	"time"

	"github.com/rm4n0s/emetos/internal/core/regex"
)

// Ported from VS Code: reference/vscode-textmate/src/grammar/tokenizeString.ts and grammar.ts
// (vscode-textmate, MIT)

// Token is a span of a line and the scopes it is in. Offsets are bytes of the line.
type Token struct {
	// Start and End are the byte offsets of the token in the line.
	Start, End int
	// Scopes are the scopes of the token, outermost first.
	Scopes *ScopeStack
}

// LineResult is the result of tokenizing a line.
type LineResult struct {
	// Tokens cover the line from its start to its end, in order. An empty line has one empty
	// token.
	Tokens []Token
	// State is where the tokenizer is at the end of the line; the next line starts from it.
	State *State
	// StoppedEarly is true when the line was not tokenized to its end, because the time limit was
	// reached or a pattern ran past its limit: the rest of the line is one token with the scopes
	// where the tokenizer stopped.
	StoppedEarly bool
}

// lineTokens collects the tokens of a line.
type lineTokens struct {
	tokens  []Token
	lastEnd int
}

// produce ends a token at endIndex, in the scopes.
func (t *lineTokens) produce(scopes *ScopeStack, endIndex int) {
	if t.lastEnd >= endIndex {
		return
	}
	t.tokens = append(t.tokens, Token{Start: t.lastEnd, End: endIndex, Scopes: scopes})
	t.lastEnd = endIndex
}

// result gives the tokens of a line that was tokenized with a line break added to it, whose
// length with it is lineLength.
func (t *lineTokens) result(st *State, lineLength int) []Token {
	if n := len(t.tokens); n > 0 && t.tokens[n-1].Start == lineLength-1 {
		// The token of the line break
		t.tokens = t.tokens[:n-1]
	}
	if len(t.tokens) == 0 {
		t.lastEnd = -1
		t.produce(st.contentScopes, lineLength)
		t.tokens[len(t.tokens)-1].Start = 0
	}
	for i := range t.tokens {
		t.tokens[i].End = min(t.tokens[i].End, lineLength-1)
		t.tokens[i].Start = min(t.tokens[i].Start, lineLength-1)
	}
	return t.tokens
}

// TokenizeLine tokenizes a line, without its line break, from the state the previous line ended
// in (nil for the first line). timeLimit, when it is not zero, is how long the tokenizer may take.
func (g *Grammar) TokenizeLine(line string, prev *State, timeLimit time.Duration) LineResult {
	g.init()
	if len(line) >= g.maxLineBytes {
		return LineResult{Tokens: []Token{{Start: 0, End: len(line)}}, State: prev}
	}
	mark := &lineMark{}
	isFirstLine := prev == nil
	if prev == nil {
		prev = g.initialState(mark)
	}
	text := line + "\n"
	t := &tokenizer{g: g, toks: &lineTokens{}, mark: mark}
	if timeLimit > 0 {
		t.deadline = time.Now().Add(timeLimit)
	}
	st, stopped := t.tokenize(regex.NewLine(text), text, isFirstLine, 0, prev, true)
	return LineResult{Tokens: t.toks.result(st, len(text)), State: st, StoppedEarly: stopped}
}

// initialState is the state of the first line: the root rule open, in the scope of the grammar.
func (g *Grammar) initialState(mark *lineMark) *State {
	var scopes *ScopeStack
	if name := g.rules[g.rootID].name; name != nil {
		scopes = scopes.pushPath(name.text, true)
	} else {
		scopes = scopes.push("unknown")
	}
	return newState(nil, g.rootID, -1, -1, false, "", false, scopes, scopes, mark)
}

// tokenizer holds what tokenizing one line needs.
type tokenizer struct {
	g        *Grammar
	toks     *lineTokens
	mark     *lineMark
	deadline time.Time
}

// produce ends a token at the end index in the scopes inside the rule of the state.
func (t *tokenizer) produce(st *State, endIndex int) {
	t.toks.produce(st.contentScopes, endIndex)
}

// tokenize tokenizes text from linePos in the state, and returns the state at the end and
// whether it stopped early. text is the whole line with its line break, or the part of it
// that a capture is tokenized again in.
func (t *tokenizer) tokenize(line *regex.Line, text string, isFirstLine bool, linePos int, st *State, checkWhile bool) (*State, bool) {
	g := t.g
	lineLength := len(text)
	anchorPos := -1
	if checkWhile {
		// A rule whose begin matched the line break of the last line is anchored at the start of
		// this one.
		if st.captured {
			anchorPos = 0
		}
		// TODO(step 4.4): check the while conditions of the open rules here.
	}
	// A grammar that does not advance is stopped by the checks below; this is for one that does
	// in a way the checks do not see.
	for guard := 0; guard < 4*lineLength+64; guard++ {
		if !t.deadline.IsZero() && time.Now().After(t.deadline) {
			return st, true
		}
		rule := g.rules[st.ruleID]
		compiled := g.compiled(rule, st.endRule, st.hasEnd)
		m, ok, err := compiled.scanner.Find(line, linePos, anchorPos, isFirstLine)
		if err != nil {
			// A pattern ran past its limit: the rest of the line is plain text.
			g.addProblem("timeout:"+rule.debugName(), err)
			t.produce(st, lineLength)
			return st, true
		}
		if !ok {
			t.produce(st, lineLength)
			return st, false
		}
		caps := m.Captures
		matchedID := compiled.ruleIDs[m.Pattern]
		hasAdvanced := caps[0].End > linePos

		if matchedID == endRuleID {
			// The end of the rule: pop it.
			popped := rule
			t.produce(st, caps[0].Start)
			st = st.withContentScopes(st.nameScopes)
			t.handleCaptures(text, isFirstLine, st, popped.endCaptures, caps)
			t.produce(st, caps[0].End)

			p := st
			st = st.parent
			anchorPos = p.anchor(t.mark)
			if !hasAdvanced && p.enter(t.mark) == linePos {
				// The grammar pushed and popped a rule without advancing. Take it as a mistake
				// of the grammar author and stay in the rule.
				st = p
				t.produce(st, lineLength)
				return st, false
			}
		} else {
			matched := g.rules[matchedID]
			t.produce(st, caps[0].Start)
			beforePush := st
			name, hasName := "", matched.name != nil
			if hasName {
				name = matched.name.value(text, caps)
			}
			nameScopes := st.contentScopes.pushPath(name, hasName)
			st = newState(st, matchedID, linePos, anchorPos, caps[0].End == lineLength, "", false, nameScopes, nameScopes, t.mark)

			switch matched.kind {
			case ruleBeginEnd:
				t.handleCaptures(text, isFirstLine, st, matched.beginCaptures, caps)
				t.produce(st, caps[0].End)
				anchorPos = caps[0].End
				content, hasContent := "", matched.contentName != nil
				if hasContent {
					content = matched.contentName.value(text, caps)
				}
				st = st.withContentScopes(nameScopes.pushPath(content, hasContent))
				if matched.end.hasBackReferences {
					st = st.withEndRule(matched.end.resolveBackReferences(text, caps))
				}
				if !hasAdvanced && beforePush.hasSameRuleAs(st, t.mark) {
					// The grammar pushed the same rule without advancing.
					st = st.parent
					t.produce(st, lineLength)
					return st, false
				}
			default:
				// A match rule: it is popped at once.
				t.handleCaptures(text, isFirstLine, st, matched.captures, caps)
				t.produce(st, caps[0].End)
				st = st.parent
				if !hasAdvanced {
					// The grammar neither advances nor pushes or pops. (safePop)
					if st.parent != nil {
						st = st.parent
					}
					t.produce(st, lineLength)
					return st, false
				}
			}
		}
		if caps[0].End > linePos {
			linePos = caps[0].End
			isFirstLine = false
		}
	}
	t.produce(st, lineLength)
	return st, true
}

// localScope is a scope that a capture opened, until the end position.
type localScope struct {
	scopes *ScopeStack
	endPos int
}

// handleCaptures gives the tokens of a match their scopes from the rules of its captures.
func (t *tokenizer) handleCaptures(text string, isFirstLine bool, st *State, captures []*rule, caps []regex.Capture) {
	if len(captures) == 0 {
		return
	}
	n := min(len(captures), len(caps))
	var local []localScope
	maxEnd := caps[0].End
	for i := 0; i < n; i++ {
		c := captures[i]
		if c == nil {
			continue
		}
		cap := caps[i]
		if cap.Start < 0 || cap.End == cap.Start {
			// Nothing was captured.
			continue
		}
		if cap.Start > maxEnd {
			// A capture beyond what the match consumed
			break
		}
		for len(local) > 0 && local[len(local)-1].endPos <= cap.Start {
			t.toks.produce(local[len(local)-1].scopes, local[len(local)-1].endPos)
			local = local[:len(local)-1]
		}
		if len(local) > 0 {
			t.toks.produce(local[len(local)-1].scopes, cap.Start)
		} else {
			t.produce(st, cap.Start)
		}

		if c.retokenizeCapturedWithRuleID != 0 {
			// The captured text is tokenized again with the rules of the capture.
			name, hasName := "", c.name != nil
			if hasName {
				name = c.name.value(text, caps)
			}
			nameScopes := st.contentScopes.pushPath(name, hasName)
			content, hasContent := "", c.contentName != nil
			if hasContent {
				content = c.contentName.value(text, caps)
			}
			contentScopes := nameScopes.pushPath(content, hasContent)
			clone := newState(st, c.retokenizeCapturedWithRuleID, cap.Start, -1, false, "", false, nameScopes, contentScopes, t.mark)
			sub := text[:cap.End]
			t.tokenize(regex.NewLine(sub), sub, isFirstLine && cap.Start == 0, cap.Start, clone, false)
			continue
		}

		if c.name != nil {
			base := st.contentScopes
			if len(local) > 0 {
				base = local[len(local)-1].scopes
			}
			local = append(local, localScope{scopes: base.pushPath(c.name.value(text, caps), true), endPos: cap.End})
		}
	}
	for len(local) > 0 {
		t.toks.produce(local[len(local)-1].scopes, local[len(local)-1].endPos)
		local = local[:len(local)-1]
	}
}

// debugName names a rule for messages.
func (r *rule) debugName() string {
	return "rule " + itoa(r.id)
}
