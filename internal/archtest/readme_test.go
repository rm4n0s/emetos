package archtest

import (
	"image/png"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode"
)

// The README is the first thing a new user reads, so its relative links and its images must resolve (step 17.1). The check
// reads the Markdown of the file, finds the links and the images, and checks that each relative target exists, that a PNG
// decodes, and that an anchor names a heading of the file it points into (headings are slugged the way GitHub does).

// linkPattern finds the target of an inline link or image: the part after "](", up to the closing parenthesis, with an optional
// title. A target in angle brackets is not used by the README and is not supported.
var linkPattern = regexp.MustCompile(`\]\(([^)\s]+)(?:\s+"[^"]*")?\)`)

// definitionPattern finds the target of a reference definition at the start of a line: [label]: target.
var definitionPattern = regexp.MustCompile(`(?m)^ {0,3}\[[^\]]+\]:\s+(\S+)`)

// markdownLinks returns the targets of the links and images of a Markdown text, leaving out fenced code blocks and code spans.
func markdownLinks(text string) []string {
	text = blankCode(text)
	var out []string
	for _, m := range linkPattern.FindAllStringSubmatch(text, -1) {
		out = append(out, m[1])
	}
	for _, m := range definitionPattern.FindAllStringSubmatch(text, -1) {
		out = append(out, m[1])
	}
	return out
}

// blankCode replaces the fenced code blocks and the code spans of a Markdown text with spaces (keeping line breaks), so that
// what looks like a link inside code is not read as one.
func blankCode(text string) string {
	var out []string
	fence := ""
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimLeft(line, " ")
		switch {
		case fence != "":
			if strings.HasPrefix(trimmed, fence) {
				fence = ""
			}
			out = append(out, "")
		case strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~"):
			fence = trimmed[:3]
			out = append(out, "")
		default:
			out = append(out, blankSpans(line))
		}
	}
	return strings.Join(out, "\n")
}

// blankSpans blanks the code spans of one line: a run of backticks up to the next run of the same length.
func blankSpans(line string) string {
	b := []byte(line)
	for i := 0; i < len(b); {
		if b[i] != '`' {
			i++
			continue
		}
		j := i
		for j < len(b) && b[j] == '`' {
			j++
		}
		ticks := j - i
		end := -1
		for k := j; k < len(b); {
			if b[k] != '`' {
				k++
				continue
			}
			m := k
			for m < len(b) && b[m] == '`' {
				m++
			}
			if m-k == ticks {
				end = m
				break
			}
			k = m
		}
		if end < 0 {
			i = j
			continue
		}
		for k := i; k < end; k++ {
			b[k] = ' '
		}
		i = end
	}
	return string(b)
}

var (
	headingPattern = regexp.MustCompile(`^ {0,3}(#{1,6})\s+(.*?)\s*#*\s*$`)
	headingLink    = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
)

// headingSlugs returns the anchors of the headings of a Markdown text as GitHub makes them: the text in lower case without
// its Markdown marks, without punctuation (letters, digits, hyphens, underscores and spaces stay), spaces as hyphens, and
// "-1", "-2" and so on for a heading that repeats.
func headingSlugs(text string) map[string]bool {
	slugs := map[string]bool{}
	seen := map[string]int{}
	fence := ""
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimLeft(line, " ")
		if fence != "" {
			if strings.HasPrefix(trimmed, fence) {
				fence = ""
			}
			continue
		}
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			fence = trimmed[:3]
			continue
		}
		m := headingPattern.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		title := headingLink.ReplaceAllString(m[2], "$1")
		var b strings.Builder
		for _, r := range strings.ToLower(title) {
			switch {
			case r == ' ':
				b.WriteByte('-')
			case r == '-' || r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r):
				b.WriteRune(r)
			}
		}
		slug := b.String()
		n := seen[slug]
		seen[slug]++
		if n > 0 {
			slug += "-" + strconv.Itoa(n)
		}
		slugs[slug] = true
	}
	return slugs
}

// checkMarkdownLinks reads the Markdown file at path (inside root) and returns one line for each link or image that does not
// resolve: a missing file, a PNG that does not decode or is empty, and an anchor that no heading has. Links with a scheme
// (https:, mailto:) are not followed: they need the network.
func checkMarkdownLinks(root, path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return []string{"cannot read " + path + ": " + err.Error()}
	}
	var problems []string
	own := headingSlugs(string(data))
	for _, target := range markdownLinks(string(data)) {
		if hasScheme(target) {
			continue
		}
		file, anchor, _ := strings.Cut(target, "#")
		file, _, _ = strings.Cut(file, "?")
		resolved := path
		if file != "" {
			resolved = filepath.Join(filepath.Dir(path), filepath.FromSlash(file))
		}
		rel, relErr := filepath.Rel(root, resolved)
		if relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			problems = append(problems, target+": points outside the repository")
			continue
		}
		info, statErr := os.Stat(resolved)
		if statErr != nil {
			problems = append(problems, target+": "+rel+" does not exist")
			continue
		}
		if !info.IsDir() && strings.EqualFold(filepath.Ext(resolved), ".png") {
			if msg := checkPNG(resolved); msg != "" {
				problems = append(problems, target+": "+msg)
			}
		}
		if anchor == "" || info.IsDir() || !strings.EqualFold(filepath.Ext(resolved), ".md") {
			continue
		}
		slugs := own
		if resolved != path {
			other, readErr := os.ReadFile(resolved)
			if readErr != nil {
				problems = append(problems, target+": cannot read "+rel)
				continue
			}
			slugs = headingSlugs(string(other))
		}
		if !slugs[anchor] {
			problems = append(problems, target+": "+rel+" has no heading with the anchor #"+anchor)
		}
	}
	slices.Sort(problems)
	return slices.Compact(problems)
}

// hasScheme says whether a link target starts with a URL scheme such as https: or mailto:.
func hasScheme(target string) bool {
	i := strings.IndexByte(target, ':')
	if i <= 0 {
		return false
	}
	for _, r := range target[:i] {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '+' && r != '-' && r != '.' {
			return false
		}
	}
	return true
}

// checkPNG returns what is wrong with a PNG file, or "" when it decodes and has a size.
func checkPNG(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return "cannot open the image: " + err.Error()
	}
	defer func() { _ = f.Close() }()
	cfg, err := png.DecodeConfig(f)
	if err != nil {
		return "is not a PNG that decodes: " + err.Error()
	}
	if cfg.Width == 0 || cfg.Height == 0 {
		return "is a PNG with no size"
	}
	return ""
}

func TestMarkdownLinkParsing(t *testing.T) {
	tests := []struct {
		name string
		text string
		want []string
	}{
		{"link and image", "[a](docs/a.md) ![b](docs/b.png)", []string{"docs/a.md", "docs/b.png"}},
		{"a title is not part of the target", `[a](docs/a.md "the title")`, []string{"docs/a.md"}},
		{"parentheses in the text of an image", "![made with (a thing)](x.png)", []string{"x.png"}},
		{"code in the text of a link", "[`CLAUDE.md`](CLAUDE.md)", []string{"CLAUDE.md"}},
		{"a code span hides a link", "use `[a](missing.md)` here and [b](b.md)", []string{"b.md"}},
		{"a double backtick span", "``[a](missing.md)`` and [b](b.md)", []string{"b.md"}},
		{"a fenced block hides a link", "```md\n[a](missing.md)\n```\n[b](b.md)", []string{"b.md"}},
		{"a tilde fence", "~~~\n[a](missing.md)\n~~~\n[b](b.md)", []string{"b.md"}},
		{"a reference definition", "[ref]: docs/ref.md\n", []string{"docs/ref.md"}},
		{"an unclosed backtick hides nothing", "a ` [b](b.md)", []string{"b.md"}},
		{"anchors", "[a](#one) [b](b.md#two)", []string{"#one", "b.md#two"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := markdownLinks(tt.text); !slices.Equal(got, tt.want) {
				t.Errorf("markdownLinks = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestHeadingSlugs(t *testing.T) {
	tests := []struct {
		name string
		text string
		want []string
	}{
		{"plain", "# Install\n", []string{"install"}},
		{"punctuation and numbers", "### 10.15 Extensions (`extension/`, `extensions/`)\n", []string{"1015-extensions-extension-extensions"}},
		{"a colon and dots", "### 9.12 Language intelligence (area 3: `core/lsp`, `gui/views/...`)\n", []string{"912-language-intelligence-area-3-corelsp-guiviews"}},
		{"a link in a heading", "## The [Go](x.md) pack\n", []string{"the-go-pack"}},
		{"a repeated heading", "# Same\n## Same\n## Same\n", []string{"same", "same-1", "same-2"}},
		{"hyphens and underscores stay", "## Where things_are - here\n", []string{"where-things_are---here"}},
		{"a closing hash run", "## Closed ##\n", []string{"closed"}},
		{"a heading in a code block is no heading", "```\n# Not a heading\n```\n# Real\n", []string{"real"}},
		{"not a heading", "#NoSpace\ntext # here\n", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := headingSlugs(tt.text)
			if len(got) != len(tt.want) {
				t.Fatalf("slugs = %v, want %q", got, tt.want)
			}
			for _, w := range tt.want {
				if !got[w] {
					t.Errorf("slugs = %v, want %q", got, w)
				}
			}
		})
	}
}

// writeFiles makes the files of a fixture tree under dir, a map of slash paths to contents.
func writeFiles(t *testing.T, dir string, files map[string][]byte) {
	t.Helper()
	for name, data := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// onePixelPNG is a PNG of one pixel.
var onePixelPNG = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01,
	0x00, 0x00, 0x00, 0x01, 0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4, 0x89, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x44, 0x41,
	0x54, 0x78, 0x9c, 0x63, 0xf8, 0xff, 0xff, 0x3f, 0x00, 0x05, 0xfe, 0x02, 0xfe, 0xa7, 0x35, 0x81, 0x84, 0x00, 0x00, 0x00,
	0x00, 0x49, 0x45, 0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
}

func TestCheckMarkdownLinks(t *testing.T) {
	tests := []struct {
		name   string
		readme string
		files  map[string][]byte
		want   []string
	}{
		{"everything resolves", "# Top\n\n[a](docs/a.md) [b](docs/a.md#section-one) [c](#top) ![i](docs/i.png) [d](docs) [e](https://example.com/x)\n",
			map[string][]byte{"docs/a.md": []byte("# A\n\n## Section One\n"), "docs/i.png": onePixelPNG}, nil},
		{"a missing file", "[a](docs/missing.md)\n", nil, []string{"docs/missing.md: docs/missing.md does not exist"}},
		{"a missing image", "![i](docs/missing.png)\n", nil, []string{"docs/missing.png: docs/missing.png does not exist"}},
		{"an image that is not a PNG", "![i](docs/i.png)\n", map[string][]byte{"docs/i.png": []byte("not an image")},
			[]string{"docs/i.png: is not a PNG that decodes: png: invalid format: not a PNG file"}},
		{"a missing anchor in the file itself", "# Top\n[a](#nowhere)\n", nil, []string{"#nowhere: README.md has no heading with the anchor #nowhere"}},
		{"a missing anchor in another file", "[a](docs/a.md#nowhere)\n", map[string][]byte{"docs/a.md": []byte("# A\n")},
			[]string{"docs/a.md#nowhere: docs/a.md has no heading with the anchor #nowhere"}},
		{"an anchor of a file that is not Markdown is not checked", "[a](docs/a.go#L10)\n", map[string][]byte{"docs/a.go": []byte("package a\n")}, nil},
		{"a link out of the repository", "[a](../outside.md)\n", nil, []string{"../outside.md: points outside the repository"}},
		{"links in code are not checked", "`[a](missing.md)`\n\n```\n[b](missing.md)\n```\n", nil, nil},
		{"a query is dropped", "[a](docs/a.md?plain=1)\n", map[string][]byte{"docs/a.md": []byte("# A\n")}, nil},
		{"the same problem is told once", "[a](x.md) [b](x.md)\n", nil, []string{"x.md: x.md does not exist"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			root := filepath.Join(dir, "repo")
			files := map[string][]byte{"README.md": []byte(tt.readme)}
			for k, v := range tt.files {
				files[k] = v
			}
			writeFiles(t, root, files)
			got := checkMarkdownLinks(root, filepath.Join(root, "README.md"))
			if !slices.Equal(got, tt.want) {
				t.Errorf("problems = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCheckMarkdownLinksReportsAFileThatCannotBeRead(t *testing.T) {
	dir := t.TempDir()
	got := checkMarkdownLinks(dir, filepath.Join(dir, "missing.md"))
	if len(got) != 1 || !strings.HasPrefix(got[0], "cannot read ") {
		t.Errorf("problems = %q", got)
	}
}

// TestREADMELinksResolve is the check of step 17.1: every relative link and image of README.md resolves, and the anchors of
// its links name headings.
func TestREADMELinksResolve(t *testing.T) {
	root, err := filepath.Abs(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range checkMarkdownLinks(root, filepath.Join(root, "README.md")) {
		t.Errorf("README.md: %s", p)
	}
}
