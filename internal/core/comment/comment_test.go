package comment

import (
	"testing"

	"github.com/rm4n0s/emetos/internal/core/language"
	"github.com/rm4n0s/emetos/internal/core/language/languagetest"
)

func TestNewLanguage(t *testing.T) {
	tests := []struct {
		name string
		cfg  *language.Configuration
		want Language
	}{
		{"nil configuration", nil, Language{}},
		{"no comments", &language.Configuration{}, Language{}},
		{"line only", &language.Configuration{Comments: &language.CommentRule{LineComment: "//"}}, Language{Line: "//"}},
		{"line without indent", &language.Configuration{Comments: &language.CommentRule{LineComment: "!", LineCommentNoIndent: true}}, Language{Line: "!", NoIndent: true}},
		{"both", &language.Configuration{Comments: &language.CommentRule{LineComment: "#", BlockComment: &language.Pair{Open: "/*", Close: "*/"}}},
			Language{Line: "#", BlockStart: "/*", BlockEnd: "*/"}},
		{"a block comment needs both tokens", &language.Configuration{Comments: &language.CommentRule{BlockComment: &language.Pair{Open: "/*"}}}, Language{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := NewLanguage(tc.cfg)
			if *got != tc.want {
				t.Errorf("NewLanguage = %+v, want %+v", *got, tc.want)
			}
			if got.HasBlock() != (tc.want.BlockStart != "") {
				t.Errorf("HasBlock() = %v", got.HasBlock())
			}
		})
	}
	var nilLanguage *Language
	if nilLanguage.HasBlock() {
		t.Error("a nil language has no block comment")
	}
}

func TestRegistryLanguages(t *testing.T) {
	reg, err := languagetest.Bundled()
	if err != nil {
		t.Fatal(err)
	}
	langs := NewRegistryLanguages(reg)
	goLang := langs.Language(reg.Code("go"))
	if goLang.Line != "//" || goLang.BlockStart != "/*" || goLang.BlockEnd != "*/" {
		t.Errorf("Go comments = %+v", *goLang)
	}
	if langs.Language(reg.Code("go")) != goLang {
		t.Error("a language is made once")
	}
	if py := langs.Language(reg.Code("python")); py.Line != "#" {
		t.Errorf("Python comments = %+v", *py)
	}
	if md := langs.Language(reg.Code("markdown")); md.Line != "" || md.BlockStart != "<!--" {
		t.Errorf("Markdown comments = %+v", *md)
	}
	if none := langs.Language(0); none.Line != "" || none.HasBlock() {
		t.Errorf("an unknown language has comments: %+v", *none)
	}
}
