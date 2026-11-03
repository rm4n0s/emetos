package jsonc

// Ported from VS Code: src/vs/base/test/common/jsonFormatter.test.ts, converted once by a script
// that is not kept. A '|' in the text marks the start and the end of the range to format.
var formatCases = []struct {
	name     string
	content  string
	expected string
	spaces   bool
}{
	{"object - single property", "{\"x\" : 1}", "{\n  \"x\": 1\n}", true},
	{"object - multiple properties", "{\"x\" : 1,  \"y\" : \"foo\", \"z\"  : true}", "{\n  \"x\": 1,\n  \"y\": \"foo\",\n  \"z\": true\n}", true},
	{"object - no properties ", "{\"x\" : {    },  \"y\" : {}}", "{\n  \"x\": {},\n  \"y\": {}\n}", true},
	{"object - nesting", "{\"x\" : {  \"y\" : { \"z\"  : { }}, \"a\": true}}", "{\n  \"x\": {\n    \"y\": {\n      \"z\": {}\n    },\n    \"a\": true\n  }\n}", true},
	{"array - single items", "[\"[]\"]", "[\n  \"[]\"\n]", true},
	{"array - multiple items", "[true,null,1.2]", "[\n  true,\n  null,\n  1.2\n]", true},
	{"array - no items", "[      ]", "[]", true},
	{"array - nesting", "[ [], [ [ {} ], \"a\" ]  ]", "[\n  [],\n  [\n    [\n      {}\n    ],\n    \"a\"\n  ]\n]", true},
	{"syntax errors", "[ null 1.2 ]", "[\n  null 1.2\n]", true},
	{"empty lines", "{\n\"a\": true,\n\n\"b\": true\n}", "{\n\t\"a\": true,\n\t\"b\": true\n}", false},
	{"single line comment", "[ \n//comment\n\"foo\", \"bar\"\n] ", "[\n  //comment\n  \"foo\",\n  \"bar\"\n]", true},
	{"block line comment", "[{\n        /*comment*/     \n\"foo\" : true\n}] ", "[\n  {\n    /*comment*/\n    \"foo\": true\n  }\n]", true},
	{"single line comment on same line", " {  \n        \"a\": {}// comment    \n } ", "{\n  \"a\": {} // comment    \n}", true},
	{"single line comment on same line 2", "{ //comment\n}", "{ //comment\n}", true},
	{"block comment on same line", "{      \"a\": {}, /*comment*/    \n        /*comment*/ \"b\": {},    \n        \"c\": {/*comment*/}    } ", "{\n  \"a\": {}, /*comment*/\n  /*comment*/ \"b\": {},\n  \"c\": { /*comment*/}\n}", true},
	{"block comment on same line advanced", " {       \"d\": [\n             null\n        ] /*comment*/\n        ,\"e\": /*comment*/ [null] }", "{\n  \"d\": [\n    null\n  ] /*comment*/,\n  \"e\": /*comment*/ [\n    null\n  ]\n}", true},
	{"multiple block comments on same line", "{      \"a\": {} /*comment*/, /*comment*/   \n        /*comment*/ \"b\": {}  /*comment*/  } ", "{\n  \"a\": {} /*comment*/, /*comment*/\n  /*comment*/ \"b\": {} /*comment*/\n}", true},
	{"multiple mixed comments on same line", "[ /*comment*/  /*comment*/   // comment \n]", "[ /*comment*/ /*comment*/ // comment \n]", true},
	{"range", "{ \"a\": {},\n|\"b\": [null, null]|\n} ", "{ \"a\": {},\n\"b\": [\n  null,\n  null\n]\n} ", true},
	{"range with existing indent", "{ \"a\": {},\n   |\"b\": [null],\n\"c\": {}\n}|", "{ \"a\": {},\n   \"b\": [\n    null\n  ],\n  \"c\": {}\n}", true},
	{"range with existing indent - tabs", "{ \"a\": {},\n|  \"b\": [null],   \n\"c\": {}\n} |    ", "{ \"a\": {},\n\t\"b\": [\n\t\tnull\n\t],\n\t\"c\": {}\n}", false},
	{"block comment none-line breaking symbols", "{ \"a\": [ 1\n/* comment */\n, 2\n/* comment */\n]\n/* comment */\n,\n \"b\": true\n/* comment */\n}", "{\n  \"a\": [\n    1\n    /* comment */\n    ,\n    2\n    /* comment */\n  ]\n  /* comment */\n  ,\n  \"b\": true\n  /* comment */\n}", true},
	{"line comment after none-line breaking symbols", "{ \"a\":\n// comment\nnull,\n \"b\"\n// comment\n: null\n// comment\n}", "{\n  \"a\":\n  // comment\n  null,\n  \"b\"\n  // comment\n  : null\n  // comment\n}", true},
}
