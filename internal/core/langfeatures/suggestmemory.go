package langfeatures

import "strings"

// MemoryMode is how the suggest widget chooses the item it focuses (editor.suggestSelection).
type MemoryMode string

// The modes.
const (
	// MemoryFirst focuses the first item, or the preselected one among the best.
	MemoryFirst MemoryMode = "first"
	// MemoryRecentlyUsed focuses the item of the best matches that was accepted last.
	MemoryRecentlyUsed MemoryMode = "recentlyUsed"
	// MemoryRecentlyUsedByPrefix focuses the item that was accepted last for the text typed before the cursor.
	MemoryRecentlyUsedByPrefix MemoryMode = "recentlyUsedByPrefix"
)

type memItem struct {
	kind       CompletionKind
	insertText string
	touch      int
}

const (
	lruCapacity = 300
	lruKeep     = lruCapacity * 66 / 100
)

// SuggestMemory remembers the suggestions that were accepted, to focus them again. It lives as long as the window tab: it is
// not saved (VS Code saves it with the state of the workspace).
//
// Ported from VS Code: src/vs/editor/contrib/suggest/browser/suggestMemory.ts
type SuggestMemory struct {
	lru    map[string]memItem
	prefix map[string]memItem
	seq    int
}

// NewSuggestMemory makes an empty memory.
func NewSuggestMemory() *SuggestMemory {
	return &SuggestMemory{lru: map[string]memItem{}, prefix: map[string]memItem{}}
}

// Memorize remembers that the item was accepted in a document of the language, with the word before the cursor.
func (m *SuggestMemory) Memorize(mode MemoryMode, languageID, wordBefore string, item *SuggestItem) {
	m.seq++
	entry := memItem{kind: item.Kind, insertText: item.EffectiveInsertText(), touch: m.seq}
	switch mode {
	case MemoryRecentlyUsed:
		m.lru[languageID+"/"+item.Label] = entry
		if len(m.lru) > lruCapacity {
			m.trim()
		}
	case MemoryRecentlyUsedByPrefix:
		m.prefix[languageID+"/"+wordBefore] = entry
	}
}

// trim drops the least recently used entries down to two thirds of the capacity.
func (m *SuggestMemory) trim() {
	for len(m.lru) > lruKeep {
		oldestKey, oldest := "", int(^uint(0)>>1)
		for k, v := range m.lru {
			if v.touch < oldest {
				oldestKey, oldest = k, v.touch
			}
		}
		delete(m.lru, oldestKey)
	}
}

// Select is the index of the item to focus among items, best first. wordBefore is the word before the cursor and
// afterSpace tells that the character before the cursor is white space.
func (m *SuggestMemory) Select(mode MemoryMode, languageID, wordBefore string, afterSpace bool, items []*SuggestItem) int {
	if len(items) == 0 {
		return 0
	}
	switch mode {
	case MemoryRecentlyUsed:
		if afterSpace {
			break
		}
		return m.selectRecent(languageID, items)
	case MemoryRecentlyUsedByPrefix:
		if wordBefore == "" {
			break
		}
		key := languageID + "/" + wordBefore
		entry, ok := m.prefix[key]
		if !ok {
			for n := len(key) - 1; n > 0 && !ok; n-- {
				entry, ok = m.prefix[key[:n]]
			}
		}
		if ok {
			for i, it := range items {
				if it.Kind == entry.kind && it.EffectiveInsertText() == entry.insertText {
					return i
				}
			}
		}
	}
	return selectPreselected(items)
}

// selectRecent is the item of the best matches that was accepted last, else the preselected one, else the first.
func (m *SuggestMemory) selectRecent(languageID string, items []*SuggestItem) int {
	top := items[0].Score.Score
	preselect, recency, seq := -1, -1, -1
	for i, it := range items {
		if it.Score.Score != top {
			break // only the best matches count
		}
		if e, ok := m.lru[languageID+"/"+it.Label]; ok && e.touch > seq && e.kind == it.Kind && e.insertText == it.EffectiveInsertText() {
			seq, recency = e.touch, i
		}
		if it.Preselect && preselect == -1 {
			return i
		}
	}
	switch {
	case recency != -1:
		return recency
	case preselect != -1:
		return preselect
	}
	return 0
}

// selectPreselected is the first item of the best matches that the provider preselected, else 0.
func selectPreselected(items []*SuggestItem) int {
	top := items[0].Score.Score
	for i, it := range items {
		if it.Score.Score != top {
			break
		}
		if it.Preselect {
			return i
		}
	}
	return 0
}

// AfterSpace tells whether the character before the byte column col of the line is white space.
func AfterSpace(line string, col int) bool {
	if col <= 0 || col > len(line) {
		return false
	}
	return strings.ContainsRune(" \t\n\r\f\v", rune(line[col-1]))
}
