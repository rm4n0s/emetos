package language

import (
	"strings"
	"testing"
)

// TestTheMongoDBPlaygroundGrammar tokenizes lines of a playground (the language of extensions/mongodb, step 20.1) with the bundled
// grammar, and checks the scopes of what a playground is made of.
func TestTheMongoDBPlaygroundGrammar(t *testing.T) {
	r, err := Bundled()
	if err != nil {
		t.Fatal(err)
	}
	g, err := r.NewGrammar("mongodb", nil)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name  string
		lines []string
		want  []string // token|scopes, skipping the tokens with no scope but the source's and the punctuation
	}{
		{"use and show", []string{"use my-shop", "show collections"}, []string{
			"use|source.mongodb keyword.control.use.mongodb", "my-shop|source.mongodb entity.name.type.database.mongodb",
			"show|source.mongodb keyword.control.show.mongodb", "collections|source.mongodb constant.language.show.mongodb"}},
		{"a find with a chain", []string{`db.orders.find({status: "open", n: {$gt: 5}}).sort({n: -1}).limit(20)`}, []string{
			"db|source.mongodb variable.language.db.mongodb", "orders|source.mongodb entity.name.type.collection.mongodb", "find|source.mongodb entity.name.function.mongodb",
			"status|source.mongodb variable.other.property.mongodb", `"|source.mongodb string.quoted.double.mongodb punctuation.definition.string.begin.mongodb`,
			"open|source.mongodb string.quoted.double.mongodb", "n|source.mongodb variable.other.property.mongodb", "$gt|source.mongodb support.type.operator.mongodb",
			"5|source.mongodb constant.numeric.decimal.mongodb", "sort|source.mongodb entity.name.function.mongodb", "-1|source.mongodb constant.numeric.decimal.mongodb"}},
		{"helpers and values", []string{`{_id: ObjectId("507f1f77bcf86cd799439011"), at: new Date(), x: MinKey, r: /^a\/b[/]/i, ok: true, no: null}`}, []string{
			"_id|source.mongodb variable.other.property.mongodb", "ObjectId|source.mongodb support.function.helper.mongodb", "new|source.mongodb keyword.operator.new.mongodb",
			"Date|source.mongodb support.function.helper.mongodb", "MinKey|source.mongodb support.function.helper.mongodb",
			`/^a\/b[/]/i|source.mongodb string.regexp.mongodb`, "true|source.mongodb constant.language.mongodb", "null|source.mongodb constant.language.mongodb"}},
		{"comments", []string{"// a note", "/* a block", "that goes on */", "use shop"}, []string{
			"//|source.mongodb comment.line.double-slash.mongodb punctuation.definition.comment.mongodb",
			" a note|source.mongodb comment.line.double-slash.mongodb",
			"/*|source.mongodb comment.block.mongodb punctuation.definition.comment.begin.mongodb",
			" a block|source.mongodb comment.block.mongodb", "that goes on |source.mongodb comment.block.mongodb",
			"*/|source.mongodb comment.block.mongodb punctuation.definition.comment.end.mongodb",
			"use|source.mongodb keyword.control.use.mongodb"}},
		{"an escape in a string", []string{`"a\n\u00e9"`}, []string{`\n|source.mongodb string.quoted.double.mongodb constant.character.escape.mongodb`, `\u00e9|source.mongodb string.quoted.double.mongodb constant.character.escape.mongodb`}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			var all []string
			for _, toks := range tokensOf(g, tt.lines...) {
				all = append(all, toks...)
			}
			joined := "\n" + strings.Join(all, "\n") + "\n"
			for _, w := range tt.want {
				if !strings.Contains(joined, "\n"+w+"\n") && !strings.Contains(joined, "\n"+w) {
					t.Errorf("no token %q in:\n%s", w, strings.Join(all, "\n"))
				}
			}
		})
	}
}
