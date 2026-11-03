package httpclient

import (
	"net/url"
	"strings"
)

// The URL box and the Params table of the request tab show the same query (SPEC §10.33 "As built (step 27.4)"). These functions
// are the pure part of keeping them in step: they work on the text of the URL and on pairs, not on boxes.

// kv is a name and a value of a query.
type kv struct{ name, value string }

// splitURL cuts a URL into what comes before the query, the query (without the "?") and the fragment (with its "#"). hasQuery says
// whether the URL has a "?".
func splitURL(raw string) (base, query, fragment string, hasQuery bool) {
	if i := strings.IndexByte(raw, '#'); i >= 0 {
		raw, fragment = raw[:i], raw[i:]
	}
	if i := strings.IndexByte(raw, '?'); i >= 0 {
		return raw[:i], raw[i+1:], fragment, true
	}
	return raw, "", fragment, false
}

// parseQuery reads the pairs of a query: "a=1&b" is (a, 1) and (b, ""). Percent escapes and "+" are decoded (a text that is not a valid
// escape is kept as it is); empty parts between "&" are skipped.
func parseQuery(query string) []kv {
	var out []kv
	for _, part := range strings.Split(query, "&") {
		if part == "" {
			continue
		}
		name, value, _ := strings.Cut(part, "=")
		out = append(out, kv{unescapeQuery(name), unescapeQuery(value)})
	}
	return out
}

// unescapeQuery decodes a part of a query; text that is not a valid escape stays as typed.
func unescapeQuery(s string) string {
	if u, err := url.QueryUnescape(s); err == nil {
		return u
	}
	return s
}

// queryEscaper escapes what would end a part of the query when it is written in the URL box; everything else (a space, a quote, a
// non-ASCII letter) stays readable, and the engine escapes it when it sends.
var queryEscaper = strings.NewReplacer("%", "%25", "&", "%26", "#", "%23", "+", "%2B")

// composeQuery writes pairs as a query; a pair without a name is left out (the engine sends none).
func composeQuery(pairs []kv) string {
	var parts []string
	for _, p := range pairs {
		if p.name == "" {
			continue
		}
		part := strings.ReplaceAll(queryEscaper.Replace(p.name), "=", "%3D")
		if p.value != "" {
			part += "=" + queryEscaper.Replace(p.value)
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, "&")
}

// urlWithQuery is the URL with its query replaced by query (no "?" at all when query is empty and the URL had none, or when the
// query is empty and the URL's own was too); the fragment stays.
func urlWithQuery(raw, query string) string {
	base, _, fragment, _ := splitURL(raw)
	if query == "" {
		return base + fragment
	}
	return base + "?" + query + fragment
}

// paramRow is a row of the Params table as far as the sync goes.
type paramRow struct {
	name, value string
	enabled     bool
}

// queryOfRows is the query of the enabled rows.
func queryOfRows(rows []paramRow) string {
	var pairs []kv
	for _, r := range rows {
		if r.enabled {
			pairs = append(pairs, kv{r.name, r.value})
		}
	}
	return composeQuery(pairs)
}

// rowsFromQuery changes the rows to match the query that was typed in the URL box: the enabled rows are replaced by the pairs of the
// query, one for one in order (so the row a person is typing in keeps its place), extra enabled rows go, extra pairs are added at
// the end; the disabled rows stay where they are. It returns, for each row of the result, the index of the row it came from, or -1
// for a new one, so that the caller keeps the boxes (and the descriptions) of the rows that stay.
func rowsFromQuery(rows []paramRow, query string) (out []paramRow, from []int) {
	pairs := parseQuery(query)
	next := 0
	for i, r := range rows {
		if !r.enabled {
			out, from = append(out, r), append(from, i)
			continue
		}
		if next < len(pairs) {
			out, from = append(out, paramRow{name: pairs[next].name, value: pairs[next].value, enabled: true}), append(from, i)
			next++
		}
	}
	for ; next < len(pairs); next++ {
		out, from = append(out, paramRow{name: pairs[next].name, value: pairs[next].value, enabled: true}), append(from, -1)
	}
	return out, from
}
