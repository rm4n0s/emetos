package emmet

import (
	"slices"
	"strings"
)

// Ported from Emmet: src/markup/format/comment.ts

// commentState is the state of the commenting of elements in HTML.
type commentState struct {
	enabled bool
	trigger []string
	before  []templateToken
	after   []templateToken
}

func newCommentState(cfg *Config) commentState {
	o := &cfg.Options
	st := commentState{enabled: o.CommentEnabled, trigger: o.CommentTrigger}
	if o.CommentBefore != "" {
		st.before = parseTemplate(o.CommentBefore)
	}
	if o.CommentAfter != "" {
		st.after = parseTemplate(o.CommentAfter)
	}
	return st
}

// commentNodeBefore writes the comment before a node, if one is needed.
func commentNodeBefore(n *Node, st *walkState) {
	if shouldComment(n, st) && st.comment.before != nil {
		outputComment(n, st.comment.before, st)
	}
}

// commentNodeAfter writes the comment after a node, if one is needed.
func commentNodeAfter(n *Node, st *walkState) {
	if shouldComment(n, st) && st.comment.after != nil {
		outputComment(n, st.comment.after, st)
	}
}

func shouldComment(n *Node, st *walkState) bool {
	c := &st.comment
	if !c.enabled || c.trigger == nil || n.Name == "" || n.Attributes == nil {
		return false
	}
	for _, a := range n.Attributes {
		if a.Name != "" && slices.Contains(c.trigger, a.Name) {
			return true
		}
	}
	return false
}

func outputComment(n *Node, tokens []templateToken, st *walkState) {
	attrs := map[string][]Value{}
	for _, a := range n.Attributes {
		if a.Name != "" && a.Value != nil {
			attrs[strings.ToUpper(a.Name)] = a.Value
		}
	}
	for _, t := range tokens {
		if !t.placeholder {
			st.out.pushString(t.text)
		} else if v, ok := attrs[t.name]; ok {
			st.out.pushString(t.before)
			pushTokens(v, st)
			st.out.pushString(t.after)
		}
	}
}
