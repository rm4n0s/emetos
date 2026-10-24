package fuzzy

import (
	"hash/maphash"
	"math"
	"unicode/utf8"
)

// ItemScore is the score of an item that has a label and an optional description (VS Code's
// IItemScore).
type ItemScore struct {
	// Score is the overall score; 0 when the item does not match.
	Score int
	// LabelMatch are the matches (offsets in runes) within the label.
	LabelMatch []Match
	// DescriptionMatch are the matches within the description.
	DescriptionMatch []Match
}

// NoItemScore is the score of an item that does not match.
var NoItemScore = ItemScore{}

// noItemScore is what the cache shares for the items that do not match; it is never changed.
var noItemScore = ItemScore{}

// ItemAccessor gives the Scorer the strings of an item to score on (VS Code's IItemAccessor).
// An empty string is a missing one.
type ItemAccessor[T any] interface {
	// ItemLabel is just the label of the item.
	ItemLabel(item T) string
	// ItemDescription is the optional description of the item.
	ItemDescription(item T) string
	// ItemPath is the path of the item when it is a file.
	ItemPath(item T) string
}

// Scores that tell what a score was made of. A label match is over LabelScoreThreshold, a
// label prefix match over LabelPrefixScoreThreshold, and the item whose path is the query
// has PathIdentityScore.
const (
	PathIdentityScore         = 1 << 18
	LabelPrefixScoreThreshold = 1 << 17
	LabelScoreThreshold       = 1 << 16
)

// Cache keeps the scores of items for one query, so that scoring the same items again (to sort
// them, after a new result of a search) does not repeat the work. It is not safe for
// concurrent use. The ItemScores it returns share their match slices with the cache: do not
// change them. Make one with NewCache.
//
// An entry is found by a 64-bit hash of the label, the description, the match mode and the
// query (VS Code uses a 32-bit hash the same way), so the cache costs 16 bytes for an item that
// does not match.
type Cache struct {
	seed maphash.Seed
	m    map[uint64]*ItemScore
}

// NewCache returns an empty Cache.
func NewCache() *Cache { return NewCacheSize(0) }

// NewCacheSize returns an empty Cache that has room for n scores.
func NewCacheSize(n int) *Cache {
	return &Cache{seed: maphash.MakeSeed(), m: make(map[uint64]*ItemScore, n)}
}

// Len is the number of scores the cache holds.
func (c *Cache) Len() int { return len(c.m) }

// mix combines two hashes; the order matters.
func mix(h, v uint64) uint64 {
	h ^= v + 0x9e3779b97f4a7c15 + (h << 6) + (h >> 2)
	return h * 0xff51afd7ed558ccd
}

// key is the hash of the inputs of a scoring: the label, the description, the match mode and
// the parts of the query that decide a score (the normalized query and the normalized value and
// the mode of every piece).
//
// Ported from VS Code: src/vs/base/common/fuzzyScorer.ts (getCacheHash); the numbers are not
// VS Code's.
func (c *Cache) key(label, description string, allowNonContiguousMatches bool, q *PreparedQuery) uint64 {
	h := maphash.String(c.seed, label)
	h = mix(h, maphash.String(c.seed, description))
	h = mix(h, maphash.String(c.seed, q.Normalized))
	if allowNonContiguousMatches {
		h = mix(h, 1)
	} else {
		h = mix(h, 2)
	}
	piece := func(p *PreparedQueryPiece) {
		if p.ExpectContiguousMatch {
			h = mix(h, 3)
		} else {
			h = mix(h, 4)
		}
		h = mix(h, maphash.String(c.seed, p.Normalized))
	}
	if len(q.Values) == 0 {
		piece(&q.PreparedQueryPiece)
	}
	for i := range q.Values {
		piece(&q.Values[i])
	}
	return h
}

// ScoreItemFuzzy scores an item against a query. It matches the query against the label first
// and, when that does not match, against the description and the label together (a path).
// The cache, which may be nil, is for one set of items and may be used for several queries.
// s may be nil, which uses a new Scorer for the call.
//
// Ported from VS Code: src/vs/base/common/fuzzyScorer.ts (scoreItemFuzzy)
func ScoreItemFuzzy[T any](s *Scorer, item T, query *PreparedQuery, allowNonContiguousMatches bool, accessor ItemAccessor[T], cache *Cache) ItemScore {
	if query == nil || query.Normalized == "" {
		return NoItemScore // we need a query to score on at least
	}
	label := accessor.ItemLabel(item)
	if label == "" {
		return NoItemScore // we need a label at least
	}
	if s == nil {
		s = new(Scorer)
	}

	description := accessor.ItemDescription(item)

	// in order to speed up scoring, we cache the score with a unique hash based on:
	// - label
	// - description (if provided)
	// - whether non-contiguous matching is enabled or not
	// - hash of the query (normalized) values
	var key uint64
	if cache != nil {
		key = cache.key(label, description, allowNonContiguousMatches, query)
		if cached, ok := cache.m[key]; ok {
			return *cached
		}
	}

	itemScore := s.doScoreItemFuzzy(label, description, accessor.ItemPath(item), query, allowNonContiguousMatches)
	if cache != nil {
		if itemScore.Score == 0 {
			cache.m[key] = &noItemScore // no allocation for the items that do not match
		} else {
			stored := itemScore
			cache.m[key] = &stored
		}
	}
	return itemScore
}

// itemRunes are the runes of the item being scored, made on demand.
type itemRunes struct {
	label, description string
	hasPath            bool

	labelR, labelLowR []rune
	labelDone         bool

	fullR, fullLowR []rune // description, separator and label
	fullDone        bool
	prefixLength    int // runes of the description and the separator

	q queryRunes
}

func (it *itemRunes) labelRunes() (r, low []rune) {
	if !it.labelDone {
		it.labelR = appendRunes(it.labelR[:0], it.label)
		it.labelLowR = appendLower(it.labelLowR[:0], it.labelR)
		it.labelDone = true
	}
	return it.labelR, it.labelLowR
}

func (it *itemRunes) fullRunes() (r, low []rune) {
	if !it.fullDone {
		full := appendRunes(it.fullR[:0], it.description)
		if it.hasPath {
			full = append(full, '/') // assume this is a file path
		}
		it.prefixLength = len(full)
		full = appendRunes(full, it.label)
		it.fullR = full
		it.fullLowR = appendLower(it.fullLowR[:0], full)
		it.fullDone = true
	}
	return it.fullR, it.fullLowR
}

func (s *Scorer) doScoreItemFuzzy(label, description, path string, query *PreparedQuery, allowNonContiguousMatches bool) ItemScore {
	preferLabelMatches := path == "" || !query.ContainsPathSeparator

	// Treat identity matches on full path highest
	if path != "" && query.PathNormalized == path {
		score := ItemScore{Score: PathIdentityScore, LabelMatch: []Match{{Start: 0, End: utf8.RuneCountInString(label)}}}
		if description != "" {
			score.DescriptionMatch = []Match{{Start: 0, End: utf8.RuneCountInString(description)}}
		}
		return score
	}

	it := &s.item
	it.label, it.description, it.hasPath = label, description, path != ""
	it.labelDone, it.fullDone = false, false
	defer func() {
		// do not hold on to big buffers
		if cap(it.fullR) > maxKeptCells || cap(it.labelR) > maxKeptCells {
			*it = itemRunes{}
		}
	}()

	// Score: multiple inputs
	if len(query.Values) > 1 {
		return s.doScoreItemFuzzyMultiple(query.Values, preferLabelMatches, allowNonContiguousMatches)
	}

	// Score: single input
	return s.doScoreItemFuzzySingle(&query.PreparedQueryPiece, preferLabelMatches, allowNonContiguousMatches)
}

// Ported from VS Code: src/vs/base/common/fuzzyScorer.ts (doScoreItemFuzzyMultiple)
func (s *Scorer) doScoreItemFuzzyMultiple(query []PreparedQueryPiece, preferLabelMatches, allowNonContiguousMatches bool) ItemScore {
	totalScore := 0
	var totalLabelMatches, totalDescriptionMatches []Match

	for i := range query {
		res := s.doScoreItemFuzzySingle(&query[i], preferLabelMatches, allowNonContiguousMatches)
		if res.Score == 0 {
			// if a single query value does not match, return with
			// no score entirely, we require all queries to match
			return NoItemScore
		}

		totalScore += res.Score
		totalLabelMatches = append(totalLabelMatches, res.LabelMatch...)
		totalDescriptionMatches = append(totalDescriptionMatches, res.DescriptionMatch...)
	}

	// if we have a score, ensure that the positions are
	// sorted in ascending order and distinct
	return ItemScore{
		Score:            totalScore,
		LabelMatch:       normalizeMatches(totalLabelMatches),
		DescriptionMatch: normalizeMatches(totalDescriptionMatches),
	}
}

// Ported from VS Code: src/vs/base/common/fuzzyScorer.ts (doScoreItemFuzzySingle)
func (s *Scorer) doScoreItemFuzzySingle(query *PreparedQueryPiece, preferLabelMatches, allowNonContiguousMatches bool) ItemScore {
	it := &s.item
	qn, ql := query.runes(&it.q)
	if len(qn) != len(ql) {
		return NoItemScore
	}
	allow := allowNonContiguousMatches && !query.ExpectContiguousMatch

	// A query that is not a subsequence of the label (or of the description and the label)
	// cannot score: most items of a search are rejected here, on the bytes, before their
	// runes are made.
	labelMay, fullMay := true, it.description != ""
	if asciiRunes(ql) {
		labelMay = mayContain(ql, "", it.label, false)
		if fullMay {
			fullMay = mayContain(ql, it.description, it.label, it.hasPath)
		}
	}

	// Prefer label matches if told so or we have no description
	if labelMay && (preferLabelMatches || it.description == "") {
		labelR, labelLowR := it.labelRunes()
		labelScore, labelPositions := s.scoreRunes(labelR, labelLowR, qn, ql, allow)
		if labelScore != 0 {

			// If we have a prefix match on the label, we give a much
			// higher baseScore to elevate these matches over others
			// This ensures that typing a file name wins over results
			// that are present somewhere in the label, but not the
			// beginning.
			var baseScore int
			var labelMatch []Match
			if hasPrefixFold(labelR, qn) {
				labelMatch = []Match{{Start: 0, End: len(qn)}}
				baseScore = LabelPrefixScoreThreshold

				// We give another boost to labels that are short, e.g. given
				// files "window.ts" and "windowActions.ts" and a query of
				// "window", we want "window.ts" to receive a higher score.
				// As such we compute the percentage the query has within the
				// label and add that to the baseScore.
				prefixLengthBoost := int(math.Round(float64(len(qn)) / float64(len(labelR)) * 100))
				baseScore += prefixLengthBoost
			} else {
				labelMatch = CreateMatches(labelPositions)
				baseScore = LabelScoreThreshold
			}

			return ItemScore{Score: baseScore + labelScore, LabelMatch: labelMatch}
		}
	}

	// Finally compute description + label scores if we have a description
	if fullMay {
		fullR, fullLowR := it.fullRunes()
		descriptionPrefixLength := it.prefixLength

		labelDescriptionScore, labelDescriptionPositions := s.scoreRunes(fullR, fullLowR, qn, ql, allow)
		if labelDescriptionScore != 0 {
			labelDescriptionMatches := CreateMatches(labelDescriptionPositions)
			labelMatch := []Match{}
			descriptionMatch := []Match{}

			// We have to split the matches back onto the label and description portions
			for _, h := range labelDescriptionMatches {

				switch {
				case h.Start < descriptionPrefixLength && h.End > descriptionPrefixLength:
					// Match overlaps label and description part, we need to split it up
					labelMatch = append(labelMatch, Match{Start: 0, End: h.End - descriptionPrefixLength})
					descriptionMatch = append(descriptionMatch, Match{Start: h.Start, End: descriptionPrefixLength})

				case h.Start >= descriptionPrefixLength:
					// Match on label part
					labelMatch = append(labelMatch, Match{Start: h.Start - descriptionPrefixLength, End: h.End - descriptionPrefixLength})

				default:
					// Match on description part
					descriptionMatch = append(descriptionMatch, h)
				}
			}

			return ItemScore{Score: labelDescriptionScore, LabelMatch: labelMatch, DescriptionMatch: descriptionMatch}
		}
	}

	return NoItemScore
}

// asciiRunes tells whether all the runes are ASCII.
func asciiRunes(r []rune) bool {
	for _, c := range r {
		if c >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

// mayContain tells whether the ASCII query (in lower case) can be a subsequence of description,
// a separator when withSep, and label, as the scorer sees them. A string that is not ASCII
// is not looked at: the answer is then true. It is the check of isSubsequence on bytes.
func mayContain(query []rune, description, label string, withSep bool) bool {
	qi := 0
	scan := func(s string) bool {
		for i := 0; i < len(s); i++ {
			c := s[i]
			if c >= utf8.RuneSelf {
				return false // not ASCII: the runes decide
			}
			if 'A' <= c && c <= 'Z' {
				c += 32
			}
			if considerAsEqual(query[qi], rune(c)) {
				qi++
				if qi == len(query) {
					return true
				}
			}
		}
		return false
	}
	if scan(description) {
		return true
	}
	if withSep && qi < len(query) && considerAsEqual(query[qi], '/') {
		qi++
		if qi == len(query) {
			return true
		}
	}
	if scan(label) {
		return true
	}
	// the scan stopped at a byte that is not ASCII, or ran out of the strings
	return !asciiString(description) || !asciiString(label)
}

func asciiString(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}
