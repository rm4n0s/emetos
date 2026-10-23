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
	// Type is the standard type of the token: comment, string, regular expression or other.
	Type TokenType
	// Language is the id of the language of the token, from the embedded languages of the
	// grammar; the initial language of the grammar for a token outside them.
	Language int
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
	g       *Grammar
	tokens  []Token
	lastEnd int
}

// produce ends a token at endIndex, in the scopes.
func (t *lineTokens) produce(scopes *ScopeStack, endIndex int) {
	if t.lastEnd >= endIndex {
		return
	}
	tok := Token{Start: t.lastEnd, End: endIndex, Scopes: scopes, Language: t.g.initialLanguage}
	if scopes != nil {
		tok.Type, tok.Language = scopes.tokenType, scopes.language
		if len(t.g.tokenTypes) > 0 {
			names := scopes.Names()
			for _, o := range t.g.tokenTypes {
				if o.match(names) {
					tok.Type = o.tokenType
				}
			}
		}
	}
	t.tokens = append(t.tokens, tok)
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
		return LineResult{Tokens: []Token{{Start: 0, End: len(line), Language: g.initialLanguage}}, State: prev}
	}
	mark := &lineMark{}
	isFirstLine := prev == nil
	if prev == nil {
		prev = g.initialState(mark)
	}
	text := line + "\n"
	t := &tokenizer{g: g, toks: &lineTokens{g: g}, mark: mark}
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
		scopes = g.push(nil, name.text)
	} else {
		// A grammar without a name for its root rule has no scope to look up.
		scopes = &ScopeStack{name: "unknown", depth: 1, language: g.initialLanguage}
	}
	return newState(nil, g.rootID, -1, -1, false, "", false, scopes, scopes, mark)
}

// tokenizer holds what tokenizing one line needs.
type tokenizer struct {
	g        *Grammar
	toks     *lineTokens
	mark     *lineMark
	deadline time.Time
	// names are the scope names of namesOf, kept for the selectors of the injections.
	namesOf *ScopeStack
	names   []string
}

// produce ends a token at the end index in the scopes inside the rule of the state.
func (t *tokenizer) produce(st *State, endIndex int) {
	t.toks.produce(st.contentScopes, endIndex)
}

// matchResult is what the scan found: the rule and the captures of its match.
type matchResult struct {
	ruleID int
	caps   []regex.Capture
	// priority is true for a match of an injection that goes before the rule of the scope.
	priority bool
}

// scan looks for the rule that matches first from linePos: the rule of the state or an injection.
func (t *tokenizer) scan(line *regex.Line, isFirstLine bool, linePos int, st *State, anchorPos int) (*matchResult, error) {
	g := t.g
	rule := g.rules[st.ruleID]
	compiled := g.compiled(rule, st.endRule, st.hasEnd)
	m, ok, err := compiled.scanner.Find(line, linePos, anchorPos, isFirstLine)
	if err != nil {
		return nil, err
	}
	var found *matchResult
	if ok {
		found = &matchResult{ruleID: compiled.ruleIDs[m.Pattern], caps: m.Captures}
	}
	if len(g.injections) == 0 {
		return found, nil
	}
	injected, err := t.scanInjections(line, isFirstLine, linePos, st, anchorPos)
	if err != nil || injected == nil {
		return found, err
	}
	if found == nil {
		return injected, nil
	}
	// The injection wins when it starts first, or when it starts together with the rule and goes
	// before the rules of the scope.
	if injected.caps[0].Start < found.caps[0].Start || (injected.priority && injected.caps[0].Start == found.caps[0].Start) {
		return injected, nil
	}
	return found, nil
}

// scanInjections looks for the injection that matches first.
func (t *tokenizer) scanInjections(line *regex.Line, isFirstLine bool, linePos int, st *State, anchorPos int) (*matchResult, error) {
	g := t.g
	bestStart := int(^uint(0) >> 1)
	var best *matchResult
	if t.namesOf != st.contentScopes || t.names == nil {
		t.namesOf, t.names = st.contentScopes, st.contentScopes.Names()
	}
	scopes := t.names
	for _, inj := range g.injections {
		if !inj.match(scopes) {
			continue
		}
		compiled := g.compiled(g.rules[inj.ruleID], "", false)
		m, ok, err := compiled.scanner.Find(line, linePos, anchorPos, isFirstLine)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		if start := m.Captures[0].Start; start < bestStart {
			bestStart = start
			best = &matchResult{ruleID: compiled.ruleIDs[m.Pattern], caps: m.Captures, priority: inj.priority == -1}
			if start == linePos {
				break
			}
		}
	}
	return best, nil
}

// checkWhile checks, from the outermost rule in, the while patterns of the open begin/while
// rules at the start of a line. A rule whose pattern does not match is closed, with the rules
// inside it; one that matches takes its part of the line.
func (t *tokenizer) checkWhile(line *regex.Line, text string, isFirstLine bool, linePos int, st *State) (*State, int, int, bool) {
	g := t.g
	anchorPos := -1
	if st.captured {
		anchorPos = 0
	}
	var whileStates []*State
	for node := st; node != nil; node = node.parent {
		if g.rules[node.ruleID].kind == ruleBeginWhile {
			whileStates = append(whileStates, node)
		}
	}
	for i := len(whileStates) - 1; i >= 0; i-- {
		node := whileStates[i]
		rule := g.rules[node.ruleID]
		compiled := g.compiledWhile(rule, node.endRule, node.hasEnd)
		m, ok, err := compiled.scanner.Find(line, linePos, anchorPos, isFirstLine)
		if err != nil {
			g.addProblem("timeout:"+rule.debugName(), err)
			ok = false
		}
		if !ok {
			st = node.parent
			break
		}
		if compiled.ruleIDs[m.Pattern] != whileRuleID {
			// This does not happen: the list has the while pattern only.
			st = node.parent
			break
		}
		caps := m.Captures
		t.produce(node, caps[0].Start)
		t.handleCaptures(text, isFirstLine, node, rule.whileCaptures, caps)
		t.produce(node, caps[0].End)
		anchorPos = caps[0].End
		if caps[0].End > linePos {
			linePos = caps[0].End
			isFirstLine = false
		}
	}
	return st, linePos, anchorPos, isFirstLine
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
		st, linePos, anchorPos, isFirstLine = t.checkWhile(line, text, isFirstLine, linePos, st)
	}
	// A grammar that does not advance is stopped by the checks below; this is for one that does
	// in a way the checks do not see.
	for guard := 0; guard < 4*lineLength+64; guard++ {
		if !t.deadline.IsZero() && time.Now().After(t.deadline) {
			return st, true
		}
		rule := g.rules[st.ruleID]
		r, err := t.scan(line, isFirstLine, linePos, st, anchorPos)
		if err != nil {
			// A pattern ran past its limit: the rest of the line is plain text.
			g.addProblem("timeout:"+rule.debugName(), err)
			t.produce(st, lineLength)
			return st, true
		}
		if r == nil {
			t.produce(st, lineLength)
			return st, false
		}
		caps := r.caps
		hasAdvanced := caps[0].End > linePos

		if r.ruleID == endRuleID {
			// The end of the rule: pop it.
			t.produce(st, caps[0].Start)
			st = st.withContentScopes(st.nameScopes)
			t.handleCaptures(text, isFirstLine, st, rule.endCaptures, caps)
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
			matched := g.rules[r.ruleID]
			t.produce(st, caps[0].Start)
			beforePush := st
			name, hasName := "", matched.name != nil
			if hasName {
				name = matched.name.value(text, caps)
			}
			nameScopes := g.pushPath(st.contentScopes, name, hasName)
			st = newState(st, r.ruleID, linePos, anchorPos, caps[0].End == lineLength, "", false, nameScopes, nameScopes, t.mark)

			switch matched.kind {
			case ruleBeginEnd, ruleBeginWhile:
				t.handleCaptures(text, isFirstLine, st, matched.beginCaptures, caps)
				t.produce(st, caps[0].End)
				anchorPos = caps[0].End
				content, hasContent := "", matched.contentName != nil
				if hasContent {
					content = matched.contentName.value(text, caps)
				}
				st = st.withContentScopes(g.pushPath(nameScopes, content, hasContent))
				switch {
				case matched.kind == ruleBeginEnd && matched.end.hasBackReferences:
					st = st.withEndRule(matched.end.resolveBackReferences(text, caps))
				case matched.kind == ruleBeginWhile && matched.while.hasBackReferences:
					st = st.withEndRule(matched.while.resolveBackReferences(text, caps))
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
	g := t.g
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
			nameScopes := g.pushPath(st.contentScopes, name, hasName)
			content, hasContent := "", c.contentName != nil
			if hasContent {
				content = c.contentName.value(text, caps)
			}
			contentScopes := g.pushPath(nameScopes, content, hasContent)
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
			local = append(local, localScope{scopes: g.pushPath(base, c.name.value(text, caps), true), endPos: cap.End})
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
