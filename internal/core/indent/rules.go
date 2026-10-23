package indent

import (
	"github.com/rm4n0s/emetos/internal/core/language"
	"github.com/rm4n0s/emetos/internal/core/regex"
)

// Ported from VS Code: src/vs/editor/common/languages/supports/indentRules.ts (MIT)

// The bits of Rules.Metadata.
const (
	// IncreaseMask is set for a line that matches increaseIndentPattern.
	IncreaseMask = 1 << iota
	// DecreaseMask is set for a line that matches decreaseIndentPattern.
	DecreaseMask
	// IndentNextLineMask is set for a line that matches indentNextLinePattern.
	IndentNextLineMask
	// UnindentMask is set for a line that matches unIndentedLinePattern.
	UnindentMask
)

// Rules are the indentation rules of a language (VS Code's IndentRulesSupport): four patterns
// that say how the lines around a line decide its indentation.
type Rules struct {
	r *language.IndentationRules
}

// NewRules makes the rules of a language configuration's indentationRules; nil when there are none.
func NewRules(r *language.IndentationRules) *Rules {
	if r == nil {
		return nil
	}
	return &Rules{r: r}
}

// matches reports whether the pattern, when there is one, finds something in the text. A search
// that fails (it ran past its time limit) finds nothing.
func matches(re *regex.Regexp, text string) bool {
	if re == nil {
		return false
	}
	_, ok, err := re.Find([]rune(text), 0)
	return err == nil && ok
}

// ShouldIncrease reports whether the lines after text are indented one level more.
func (r *Rules) ShouldIncrease(text string) bool {
	return r != nil && matches(r.r.IncreaseIndentPattern, text)
}

// ShouldDecrease reports whether the line text is indented one level less than the line before it.
func (r *Rules) ShouldDecrease(text string) bool {
	return r != nil && matches(r.r.DecreaseIndentPattern, text)
}

// ShouldIndentNextLine reports whether only the line after text is indented one level more.
func (r *Rules) ShouldIndentNextLine(text string) bool {
	return r != nil && matches(r.r.IndentNextLinePattern, text)
}

// ShouldIgnore reports whether the indentation of the line text is left alone
// (unIndentedLinePattern).
func (r *Rules) ShouldIgnore(text string) bool {
	return r != nil && matches(r.r.UnIndentedLinePattern, text)
}

// Metadata is the bits of the rules that text matches.
func (r *Rules) Metadata(text string) int {
	n := 0
	if r.ShouldIncrease(text) {
		n |= IncreaseMask
	}
	if r.ShouldDecrease(text) {
		n |= DecreaseMask
	}
	if r.ShouldIndentNextLine(text) {
		n |= IndentNextLineMask
	}
	if r.ShouldIgnore(text) {
		n |= UnindentMask
	}
	return n
}
