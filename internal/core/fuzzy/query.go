package fuzzy

import "strings"

// pathSeparator is the separator of paths: Emetos is for Linux. A backslash in a query is
// taken for a slash.
const pathSeparator = "/"

// multipleQueryValuesSeparator splits a query in pieces.
const multipleQueryValuesSeparator = " "

// PreparedQueryPiece is one word of a query (or all of a query without spaces).
type PreparedQueryPiece struct {
	// Original is the query as provided as input.
	Original          string
	OriginalLowercase string

	// PathNormalized is Original with the backslashes turned into slashes.
	PathNormalized string

	// Normalized is PathNormalized with white space, wildcards (*), quotes, ellipsis (…)
	// and a trailing hash character removed.
	Normalized          string
	NormalizedLowercase string

	// ExpectContiguousMatch is set when the query is wrapped in quotes, which means that it
	// must be a substring of the input: no fuzzy matching is used.
	ExpectContiguousMatch bool

	// the runes of Normalized and NormalizedLowercase, made once by PrepareQuery
	normRunes, normLowerRunes []rune
}

// PreparedQuery is a query ready for scoring many strings (VS Code's IPreparedQuery). Make it
// with PrepareQuery and do not change the strings of its pieces.
type PreparedQuery struct {
	PreparedQueryPiece

	// Values is the query split by spaces into pieces; nil when the query is one piece.
	Values []PreparedQueryPiece

	// ContainsPathSeparator tells whether the query has a path separator (a slash or a
	// backslash).
	ContainsPathSeparator bool
}

// queryExpectsExactMatch: if a query is wrapped in quotes, the user does not want to use fuzzy
// search for this query.
func queryExpectsExactMatch(query string) bool {
	return strings.HasPrefix(query, `"`) && strings.HasSuffix(query, `"`)
}

// PrepareQuery prepares a search value for scoring by removing unwanted characters and
// allowing to score on multiple pieces separated by spaces.
//
// Ported from VS Code: src/vs/base/common/fuzzyScorer.ts (prepareQuery)
func PrepareQuery(original string) *PreparedQuery {
	piece := newPiece(original)
	q := &PreparedQuery{
		PreparedQueryPiece:    piece,
		ContainsPathSeparator: strings.Contains(piece.PathNormalized, pathSeparator),
	}

	originalSplit := strings.Split(original, multipleQueryValuesSeparator)
	if len(originalSplit) > 1 {
		for _, originalPiece := range originalSplit {
			p := newPiece(originalPiece)
			if p.Normalized != "" {
				q.Values = append(q.Values, p)
			}
		}
	}
	return q
}

func newPiece(original string) PreparedQueryPiece {
	pathNormalized, normalized, normalizedLowercase := normalizeQuery(original)
	p := PreparedQueryPiece{
		Original:              original,
		OriginalLowercase:     strings.ToLower(original),
		PathNormalized:        pathNormalized,
		Normalized:            normalized,
		NormalizedLowercase:   normalizedLowercase,
		ExpectContiguousMatch: queryExpectsExactMatch(original),
	}
	p.normRunes = appendRunes(nil, normalized)
	p.normLowerRunes = appendLower(make([]rune, 0, len(p.normRunes)), p.normRunes)
	return p
}

// normalizeQuery removes the characters that help to find better results:
//   - quotes: are used for exact match search
//   - wildcards: are used for fuzzy matching
//   - whitespace: are used to separate queries
//   - ellipsis: sometimes used to indicate any path segments
//   - trailing hash: used by some language servers (e.g. rust-analyzer) as query modifiers
//
// Ported from VS Code: src/vs/base/common/fuzzyScorer.ts (normalizeQuery)
func normalizeQuery(original string) (pathNormalized, normalized, normalizedLowercase string) {
	// Help macOS/Linux users to search for paths when using backslash
	pathNormalized = strings.ReplaceAll(original, `\`, pathSeparator)

	runes := make([]rune, 0, len(pathNormalized))
	for _, r := range pathNormalized {
		if r == '*' || r == '…' || r == '"' || jsSpace(r) {
			continue
		}
		runes = append(runes, r)
	}
	// a trailing hash goes, unless it is the only character
	if n := len(runes); n > 1 && runes[n-1] == '#' {
		runes = runes[:n-1]
	}
	normalized = string(runes)
	return pathNormalized, normalized, string(appendLower(make([]rune, 0, len(runes)), runes))
}

// PieceToQuery makes the query of a piece.
//
// Ported from VS Code: src/vs/base/common/fuzzyScorer.ts (pieceToQuery)
func PieceToQuery(piece *PreparedQueryPiece) *PreparedQuery {
	return PrepareQuery(piece.Original)
}

// PiecesToQuery makes the query of pieces: the query of their originals joined by spaces.
//
// Ported from VS Code: src/vs/base/common/fuzzyScorer.ts (pieceToQuery)
func PiecesToQuery(pieces []PreparedQueryPiece) *PreparedQuery {
	originals := make([]string, len(pieces))
	for i := range pieces {
		originals[i] = pieces[i].Original
	}
	return PrepareQuery(strings.Join(originals, multipleQueryValuesSeparator))
}

// queryRunes are scratch runes for the pieces made by hand, without PrepareQuery.
type queryRunes struct{ norm, lower []rune }

// runes returns the runes of the normalized piece and of its lower case form.
func (p *PreparedQueryPiece) runes(buf *queryRunes) (norm, lower []rune) {
	if len(p.normRunes) > 0 || p.Normalized == "" {
		return p.normRunes, p.normLowerRunes
	}
	buf.norm = appendRunes(buf.norm[:0], p.Normalized)
	buf.lower = appendRunes(buf.lower[:0], p.NormalizedLowercase)
	return buf.norm, buf.lower
}
