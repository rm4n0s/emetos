package edit

import (
	"strings"
	"unicode/utf8"

	"github.com/rm4n0s/emetos/internal/core/autoclose"
	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/textmate"
	"github.com/rm4n0s/emetos/internal/core/tokens"
)

// InsertionTypes is what a TokenSource can also do: tell the standard type the text ch would
// have if it were inserted at a byte column of a line (tokens.Tokenizer.TypeIfInserting). The
// auto-closing of quotes asks it, to see whether a quote would end a string or begin one.
type InsertionTypes interface {
	TypeIfInserting(line, col int, ch string) (textmate.TokenType, bool)
}

// typeAutoClosing types the single character ch as the auto-closing settings say: over the
// closing character in front of the cursor, with its closing pair after it, or around the
// selection. It reports false, and does nothing, when none of them applies.
//
// Ported from VS Code: src/vs/editor/common/cursor/cursorTypeEditOperations.ts
// (AutoClosingOvertypeOperation, AutoClosingOpenCharTypeOperation, SurroundSelectionOperation)
func (c Commands) typeAutoClosing(sels []buffer.Selection, ch string) (Result, bool) {
	if c.Options.AutoClosing.Empty() {
		return Result{}, false
	}
	if res, ok := c.autoClosingOvertype(sels, ch); ok {
		return res, true
	}
	if res, ok := c.autoClosingOpen(sels, ch); ok {
		return res, true
	}
	return c.surroundSelection(sels, ch)
}

// autoClosed reports whether a closing character the editor typed starts at pos.
func (c Commands) autoClosed(pos buffer.Position) bool {
	for _, r := range c.Options.AutoClosed {
		if r.Start == pos {
			return true
		}
	}
	return false
}

// autoClosingOvertype types ch over the identical closing character after every cursor.
func (c Commands) autoClosingOvertype(sels []buffer.Selection, ch string) (Result, bool) {
	o := c.Options
	if o.AutoClosingOvertype == autoclose.NeverAct || !o.AutoClosing.IsCloseSingleChar(ch) {
		return Result{}, false
	}
	plans := make([]plan, len(sels))
	for i, s := range sels {
		s = c.validSelection(s)
		if !s.IsEmpty() {
			return Result{}, false
		}
		pos := s.Active
		line := c.Model.LineContent(pos.Line)
		if !strings.HasPrefix(line[pos.Col:], ch) {
			return Result{}, false
		}
		// Do not type over a quote after a backslash.
		if autoclose.IsQuote(ch) && pos.Col >= 2 && line[pos.Col-1] == '\\' {
			return Result{}, false
		}
		// The closing character must be one the editor typed.
		if o.AutoClosingOvertype == autoclose.Auto && !c.autoClosed(pos) {
			return Result{}, false
		}
		plans[i] = replacePlan(buffer.Range{Start: pos, End: pos.Delta(0, len(ch))}, ch)
	}
	return build(sels, plans, document.KindTyping), true
}

// autoClosingOpen types ch and, when it completes the opening of an auto-closing pair, the
// closing text after the cursor.
func (c Commands) autoClosingOpen(sels []buffer.Selection, ch string) (Result, bool) {
	pair, ok := c.autoClosingPair(sels, ch)
	if !ok {
		return Result{}, false
	}
	closing := pair.closing()
	if len(pair.Open) > len(ch) {
		// An opening of several characters: first the character is typed, and the closing text
		// follows as a step of its own, so that undo takes only the closing text away.
		plans := make([]plan, len(sels))
		for i, s := range sels {
			plans[i] = replacePlan(c.validRange(s), ch)
		}
		res := build(sels, plans, document.KindOther)
		res.FollowUp = closingFollowUp(len(sels), ch, closing)
		return res, true
	}
	return c.insertClosing(sels, ch, closing, true), true
}

// closingFollowUp is the second step of the typing of an opening of several characters: the
// closing text goes after the characters just typed, at each of the n cursors.
func closingFollowUp(n int, ch, closing string) func(Commands, []buffer.Selection) (Result, bool) {
	return func(next Commands, now []buffer.Selection) (Result, bool) {
		if len(now) != n {
			return Result{}, false
		}
		for _, s := range now {
			if !s.IsEmpty() || s.Active.Col < len(ch) || next.Model.LineContent(s.Active.Line)[s.Active.Col-len(ch):s.Active.Col] != ch {
				return Result{}, false
			}
		}
		return next.insertClosing(now, ch, closing, false), true
	}
}

// closedAt describes the closing text that ends at end, after the typed character ch.
func closedAt(end buffer.Position, ch, closing string) AutoClosed {
	return AutoClosed{
		Character: buffer.Range{Start: end.Delta(0, -len(closing)), End: end},
		Enclosing: buffer.Range{Start: end.Delta(0, -len(closing)-len(ch)), End: end},
	}
}

// insertClosing inserts closing (and, with typed, the character ch before it) at every cursor, which
// ends before the closing text, and reports it as auto-closed.
//
// Ported from VS Code: cursorTypeEditOperations.ts (TypeWithAutoClosingCommand)
func (c Commands) insertClosing(sels []buffer.Selection, ch, closing string, typed bool) Result {
	text := closing
	if typed {
		text = ch + closing
	}
	plans := make([]plan, len(sels))
	for i, s := range sels {
		p := replacePlan(c.validRange(s), text)
		p.selection = func(_ *mapper, now []buffer.Range) buffer.Selection {
			return buffer.NewCursor(now[0].End.Delta(0, -len(closing)))
		}
		if closing != "" {
			p.closed = func(now []buffer.Range) AutoClosed { return closedAt(now[0].End, ch, closing) }
		}
		plans[i] = p
	}
	return build(sels, plans, document.KindOther)
}

// foundPair is an auto-closing pair and what typing its last character at the cursors makes of it.
type foundPair struct {
	*autoclose.Pair
	// contained says that the closing text of a pair inside this one (the ")" of "(" in "(*",
	// "*)") already follows every cursor; containedClose is its text and is left out of what is
	// inserted.
	contained      bool
	containedClose string
}

// closing is the closing text to insert: that of the pair, less the part of it that follows the
// cursors already.
func (p foundPair) closing() string {
	if p.contained {
		return p.Close[:len(p.Close)-len(p.containedClose)]
	}
	return p.Close
}

// autoClosingPair finds the pair to close when ch is typed at every cursor.
//
// Ported from VS Code: cursorTypeEditOperations.ts (AutoClosingOpenCharTypeOperation.getAutoClosingPair)
func (c Commands) autoClosingPair(sels []buffer.Selection, ch string) (foundPair, bool) {
	o := c.Options
	positions := make([]buffer.Position, len(sels))
	for i, s := range sels {
		s = c.validSelection(s)
		if !s.IsEmpty() {
			return foundPair{}, false
		}
		positions[i] = s.Active
	}
	pair := c.findPairOpen(positions, ch)
	if pair == nil {
		return foundPair{}, false
	}
	var strategy autoclose.Strategy
	var shouldCloseBefore func(string) bool
	checkBalance := false
	if autoclose.IsQuote(ch) {
		strategy = o.AutoClosingQuotes
		shouldCloseBefore = o.AutoClosing.ShouldCloseBefore(strategy, true)
	} else if start := o.AutoClosing.BlockCommentStart(); start != "" && strings.Contains(pair.Open, start) {
		strategy = o.AutoClosingComments
		shouldCloseBefore = o.AutoClosing.ShouldCloseBefore(strategy, false)
	} else {
		strategy = o.AutoClosingBrackets
		shouldCloseBefore = o.AutoClosing.ShouldCloseBefore(strategy, false)
		checkBalance = true
	}
	if strategy == autoclose.Never {
		return foundPair{}, false
	}
	// Two pairs may contain one another, as ( ) and (* *): after "(" the text is "(|)", and the
	// "*" typed there makes "(*|*)" and not "(*|*))".
	contained := c.findContainedPair(pair)
	containedClose := ""
	if contained != nil {
		containedClose = contained.Close
	}
	containedPresent := true
	for _, pos := range positions {
		line := c.Model.LineContent(pos.Line)
		before, after := line[:pos.Col], line[pos.Col:]
		if !strings.HasPrefix(after, containedClose) {
			containedPresent = false
		}
		// Close only before an allowed character, or before the closing text of another pair.
		if after != "" {
			next, _ := utf8.DecodeRuneInString(after)
			if !c.isBeforeClosingBrace(after) && !shouldCloseBefore(string(next)) {
				return foundPair{}, false
			}
		}
		if checkBalance && strategy != autoclose.Always && o.UnmatchedClosing != nil && o.UnmatchedClosing(pos, pair.Open) {
			return foundPair{}, false
		}
		// Do not close ' or " after a word character.
		if len(pair.Open) == 1 && (ch == "'" || ch == "\"") && strategy != autoclose.Always && before != "" {
			prev, _ := utf8.DecodeLastRuneInString(before)
			if o.words().Class(prev) == ClassRegular {
				return foundPair{}, false
			}
		}
		if !c.cheapToTokenize(pos.Line) {
			return foundPair{}, false
		}
		if !c.pairAllowedAt(pair, pos) {
			return foundPair{}, false
		}
		// A quote could begin a string, which is when to close it, or end one. The tokenizer tells
		// what a neutral character typed there would be in.
		if n := pair.Neutral(); n != "" && o.Tokens != nil {
			if it, ok := o.Tokens.(InsertionTypes); ok {
				if t, known := it.TypeIfInserting(pos.Line, pos.Col, n); known && !pair.OK(t) {
					return foundPair{}, false
				}
			}
		}
	}
	return foundPair{Pair: pair, contained: containedPresent && contained != nil, containedClose: containedClose}, true
}

// findPairOpen finds the pair whose opening is completed by typing ch at every position: of
// several, the one with the longest opening.
func (c Commands) findPairOpen(positions []buffer.Position, ch string) *autoclose.Pair {
	var result *autoclose.Pair
	for _, cand := range c.Options.AutoClosing.OpenByEnd(ch) {
		if result != nil && len(cand.Open) <= len(result.Open) {
			continue
		}
		head := strings.TrimSuffix(cand.Open, ch)
		match := true
		for _, pos := range positions {
			prefix := c.Model.LineContent(pos.Line)[:pos.Col]
			if !strings.HasSuffix(prefix, head) || !cand.BeforeTextMatches(prefix+ch) {
				match = false
				break
			}
		}
		if match {
			result = cand
		}
	}
	return result
}

// findContainedPair finds another pair that the pair contains: its opening is in the opening of
// the pair and its closing ends the closing of the pair.
func (c Commands) findContainedPair(pair *autoclose.Pair) *autoclose.Pair {
	if len(pair.Open) <= 1 {
		return nil
	}
	var result *autoclose.Pair
	for _, cand := range c.Options.AutoClosing.CloseByEnd(pair.Close[max(len(pair.Close)-1, 0):]) {
		if cand.Open != pair.Open && strings.Contains(pair.Open, cand.Open) && strings.HasSuffix(pair.Close, cand.Close) {
			if result == nil || len(cand.Open) > len(result.Open) {
				result = cand
			}
		}
	}
	return result
}

// isBeforeClosingBrace reports whether the text starts with the closing of a pair and not with the
// opening of one.
func (c Commands) isBeforeClosingBrace(after string) bool {
	next, _ := utf8.DecodeRuneInString(after)
	lang := c.Options.AutoClosing
	for _, p := range lang.OpenByStart(string(next)) {
		if strings.HasPrefix(after, p.Open) {
			return false
		}
	}
	for _, p := range lang.CloseByStart(string(next)) {
		if strings.HasPrefix(after, p.Close) {
			return true
		}
	}
	return false
}

// pairAllowedAt reports whether the pair is closed in the token before the cursor.
//
// Ported from VS Code: src/vs/editor/common/languages/languageConfiguration.ts
// (StandardAutoClosingPairConditional.shouldAutoClose)
func (c Commands) pairAllowedAt(pair *autoclose.Pair, pos buffer.Position) bool {
	if c.Options.Tokens == nil || c.Model.LineContent(pos.Line) == "" {
		return true
	}
	toks := c.Options.Tokens.LineTokens(pos.Line)
	if len(toks) == 0 {
		return true
	}
	// Only the tokens of the language at the cursor count (an embedded language is a scope of its
	// own): the token before the cursor in them decides.
	at := tokenAt(toks, pos.Col)
	first := at
	for first > 0 && toks[first-1].Language == toks[at].Language {
		first--
	}
	runStart := 0
	if first > 0 {
		runStart = int(toks[first-1].End)
	}
	return pair.OK(toks[tokenAt(toks, max(pos.Col-1, runStart))].Type)
}

// tokenAt is the index of the token that has the byte offset col, or of the last one.
func tokenAt(toks []tokens.Token, col int) int {
	for i, t := range toks {
		if col < int(t.End) {
			return i
		}
	}
	return len(toks) - 1
}

// shouldSurroundChar reports whether typing ch over a selection may surround it.
func shouldSurroundChar(s autoclose.Surround, ch string) bool {
	if autoclose.IsQuote(ch) {
		return s == autoclose.SurroundQuotes || s == autoclose.SurroundLanguageDefined
	}
	return s == autoclose.SurroundBrackets || s == autoclose.SurroundLanguageDefined
}

// surroundSelection types ch over the selections: the opening character goes before each and the
// closing one after it, and the selection stays on the text between them (editor.autoSurround).
//
// Ported from VS Code: cursorTypeEditOperations.ts (SurroundSelectionOperation) and
// src/vs/editor/common/commands/surroundSelectionCommand.ts
func (c Commands) surroundSelection(sels []buffer.Selection, ch string) (Result, bool) {
	closing, ok := c.Options.AutoClosing.SurroundingClose(ch)
	if !ok || !shouldSurroundChar(c.Options.AutoSurround, ch) {
		return Result{}, false
	}
	plans := make([]plan, len(sels))
	for i, s := range sels {
		r := c.validRange(s)
		if r.IsEmpty() || c.onlyBlank(r) {
			return Result{}, false
		}
		// A quote typed over a single quote character replaces it.
		if autoclose.IsQuote(ch) && r.Start.Line == r.End.Line && r.End.Col-r.Start.Col == 1 &&
			autoclose.IsQuote(c.Model.LineContent(r.Start.Line)[r.Start.Col:r.End.Col]) {
			return Result{}, false
		}
		plans[i] = plan{
			edits: []document.Edit{
				{Range: buffer.Range{Start: r.Start, End: r.Start}, Text: ch},
				{Range: buffer.Range{Start: r.End, End: r.End}, Text: closing},
			},
			selection: func(_ *mapper, now []buffer.Range) buffer.Selection {
				return buffer.Selection{Anchor: now[0].End, Active: now[1].End.Delta(0, -len(closing))}
			},
		}
	}
	return build(sels, plans, document.KindOther), true
}

// onlyBlank reports whether the text in r has nothing but spaces and tabs (and line breaks).
func (c Commands) onlyBlank(r buffer.Range) bool {
	for line := r.Start.Line; line <= r.End.Line; line++ {
		text := c.Model.LineContent(line)
		from, to := 0, len(text)
		if line == r.Start.Line {
			from = r.Start.Col
		}
		if line == r.End.Line {
			to = r.End.Col
		}
		if strings.Trim(text[from:to], " \t") != "" {
			return false
		}
	}
	return true
}

// autoClosingPairDelete reports whether Backspace deletes the opening character before every
// cursor together with the closing one after it, when they form an auto-closing pair of one
// character each.
//
// Ported from VS Code: src/vs/editor/common/cursor/cursorDeleteOperations.ts (isAutoClosingPairDelete)
func (c Commands) autoClosingPairDelete(sels []buffer.Selection) bool {
	o := c.Options
	if o.AutoClosing.Empty() || o.AutoClosingDelete == autoclose.NeverAct ||
		(o.AutoClosingBrackets == autoclose.Never && o.AutoClosingQuotes == autoclose.Never) {
		return false
	}
	for _, s := range sels {
		s = c.validSelection(s)
		if !s.IsEmpty() {
			return false
		}
		pos := s.Active
		line := c.Model.LineContent(pos.Line)
		if pos.Col < 1 || pos.Col >= len(line) {
			return false
		}
		r, _ := utf8.DecodeLastRuneInString(line[:pos.Col])
		character := string(r)
		candidates := o.AutoClosing.OpenByEnd(character)
		if len(candidates) == 0 {
			return false
		}
		if autoclose.IsQuote(character) {
			if o.AutoClosingQuotes == autoclose.Never {
				return false
			}
		} else if o.AutoClosingBrackets == autoclose.Never {
			return false
		}
		a, _ := utf8.DecodeRuneInString(line[pos.Col:])
		found := false
		for _, cand := range candidates {
			if cand.Open == character && cand.Close == string(a) {
				found = true
			}
		}
		if !found {
			return false
		}
		// Only a pair the editor typed, unless the setting says always.
		if o.AutoClosingDelete == autoclose.Auto && !c.autoClosed(pos) {
			return false
		}
	}
	return true
}

// deleteAutoClosingPair deletes the character before every cursor and the one after it.
func (c Commands) deleteAutoClosingPair(sels []buffer.Selection) Result {
	plans := make([]plan, len(sels))
	for i, s := range sels {
		pos := c.validSelection(s).Active
		line := c.Model.LineContent(pos.Line)
		_, before := utf8.DecodeLastRuneInString(line[:pos.Col])
		_, after := utf8.DecodeRuneInString(line[pos.Col:])
		plans[i] = replacePlan(buffer.Range{Start: pos.Delta(0, -before), End: pos.Delta(0, after)}, "")
	}
	return build(sels, plans, document.KindDeleteLeft)
}
