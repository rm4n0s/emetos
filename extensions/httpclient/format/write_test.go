package format

import (
	"testing"
)

func TestMarshalFixedKeyOrder(t *testing.T) {
	req := Request{
		Name: "Get <user> & co", Method: "GET", URL: "{{base}}/users/1", Description: "d",
		Headers: []Pair{{Name: "Accept", Value: "*/*", Enabled: true}, {Name: "X-Off", Value: "1"}},
		Auth:    &Auth{Type: AuthInherit},
		Body:    &Body{Type: BodyJSON, File: "get-user.body.json"},
		Settings: &Settings{
			FollowRedirects: ptr(false),
		},
		Checks: []Check{{Subject: "status", Op: "equals", Value: 200}},
	}
	want := `{
  "emetosHttp": 1,
  "name": "Get <user> & co",
  "description": "d",
  "method": "GET",
  "url": "{{base}}/users/1",
  "headers": [
    {
      "name": "Accept",
      "value": "*/*"
    },
    {
      "name": "X-Off",
      "value": "1",
      "enabled": false
    }
  ],
  "auth": {
    "type": "inherit"
  },
  "body": {
    "type": "json",
    "file": "get-user.body.json"
  },
  "settings": {
    "followRedirects": false
  },
  "checks": [
    {
      "subject": "status",
      "op": "equals",
      "value": 200
    }
  ]
}
`
	got, err := Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("Marshal =\n%s\nwant\n%s", got, want)
	}

	coll, err := Marshal(Collection{Name: "API", Order: []string{"b.request.jsonc"}, Variables: []Variable{{Name: "k", Enabled: true, Secret: true, Value: "never"}}, Headers: []Pair{{Name: "A", Value: "b", Enabled: true}}, Auth: &Auth{Type: AuthNone}})
	if err != nil {
		t.Fatal(err)
	}
	wantColl := `{
  "emetosHttp": 1,
  "name": "API",
  "order": [
    "b.request.jsonc"
  ],
  "variables": [
    {
      "name": "k",
      "secret": true
    }
  ],
  "auth": {
    "type": "none"
  },
  "headers": [
    {
      "name": "A",
      "value": "b"
    }
  ]
}
`
	if coll != wantColl {
		t.Errorf("Marshal(collection) =\n%s\nwant\n%s", coll, wantColl)
	}
}

func TestMarshalRefusesWhatTheFormatDoesNotAllow(t *testing.T) {
	_, err := Marshal(Request{Name: "x", Body: &Body{Type: "yaml"}})
	appErr := routeErr(t, err, "Marshal")
	wantRoute(t, err, "HttpFileValueInvalid")
	if appErr.Metadata["key"] != "body" {
		t.Errorf("key = %v", appErr.Metadata["key"])
	}
	_, err = Update(`{"emetosHttp": 1}`, Environment{Variables: []Variable{{}}})
	wantRoute(t, err, "Update")
	wantRoute(t, err, "HttpFileValueInvalid")
}

func TestUpdateKeepsCommentsAndUnknownKeys(t *testing.T) {
	tests := []struct {
		name   string
		text   string
		change func(r *Request)
		want   string
	}{
		{
			name: "nothing changed leaves the text alone",
			text: "// top\n{\n\t\"emetosHttp\": 1, // version\n\t\"name\": \"A\",\n\t\"method\": \"GET\",\n\t\"url\": \"u\",\n\t\"note\": [1, 2,],\n}\n",
			want: "// top\n{\n\t\"emetosHttp\": 1, // version\n\t\"name\": \"A\",\n\t\"method\": \"GET\",\n\t\"url\": \"u\",\n\t\"note\": [1, 2,],\n}\n",
		},
		{
			name:   "a changed value keeps its neighbours and comments",
			text:   "{\n  // the verb\n  \"emetosHttp\": 1,\n  \"name\": \"A\",\n  \"method\": \"GET\", // verb\n  \"url\": \"u\",\n  \"x-team\": \"core\"\n}\n",
			change: func(r *Request) { r.Method = "POST" },
			want:   "{\n  // the verb\n  \"emetosHttp\": 1,\n  \"name\": \"A\",\n  \"method\": \"POST\", // verb\n  \"url\": \"u\",\n  \"x-team\": \"core\"\n}\n",
		},
		{
			name:   "a new key goes after the keys that come before it in the order",
			text:   "{\n  \"emetosHttp\": 1,\n  \"name\": \"A\",\n  \"method\": \"GET\",\n  \"url\": \"u\",\n  \"checks\": []\n}\n",
			change: func(r *Request) { r.Headers = []Pair{{Name: "A", Value: "b", Enabled: true}}; r.Checks = nil },
			want:   "{\n  \"emetosHttp\": 1,\n  \"name\": \"A\",\n  \"method\": \"GET\",\n  \"url\": \"u\",\n  \"headers\": [\n    {\n      \"name\": \"A\",\n      \"value\": \"b\"\n    }\n  ]\n}\n",
		},
		{
			name:   "a key the document no longer has is removed, the unknown ones stay",
			text:   "{\n  \"emetosHttp\": 1,\n  \"name\": \"A\",\n  \"description\": \"old\",\n  \"method\": \"GET\",\n  \"url\": \"u\",\n  \"order\": [\"x\"]\n}\n",
			change: func(r *Request) { r.Description = "" },
			want:   "{\n  \"emetosHttp\": 1,\n  \"name\": \"A\",\n  \"method\": \"GET\",\n  \"url\": \"u\",\n  \"order\": [\"x\"]\n}\n",
		},
		{
			name: "an unknown key of a header and its comment stay when the value changes",
			text: "{\"emetosHttp\": 1, \"name\": \"A\", \"method\": \"GET\", \"url\": \"u\",\n \"headers\": [\n  {\"name\": \"A\", /* why */ \"value\": \"1\", \"x-lint\": true}\n ]}",
			change: func(r *Request) {
				r.Headers = []Pair{{Name: "A", Value: "2", Enabled: true}}
			},
			want: "{\"emetosHttp\": 1, \"name\": \"A\", \"method\": \"GET\", \"url\": \"u\",\n \"headers\": [\n  {\"name\": \"A\", /* why */ \"value\": \"2\", \"x-lint\": true}\n ]}",
		},
		{
			name: "a header is disabled and another one added",
			text: "{\"emetosHttp\": 1, \"name\": \"A\", \"method\": \"GET\", \"url\": \"u\", \"headers\": [{\"name\": \"A\", \"value\": \"1\"}]}",
			change: func(r *Request) {
				r.Headers[0].Enabled = false
				r.Headers = append(r.Headers, Pair{Name: "B", Value: "2", Enabled: true})
			},
			want: "{\n  \"emetosHttp\": 1,\n  \"name\": \"A\",\n  \"method\": \"GET\",\n  \"url\": \"u\",\n  \"headers\": [\n    {\n      \"name\": \"A\",\n      \"value\": \"1\",\n      \"enabled\": false\n    },\n    {\n      \"name\": \"B\",\n      \"value\": \"2\"\n    }\n  ]\n}",
		},
		{
			name:   "an element removed from the end of a list",
			text:   "{\"emetosHttp\": 1, \"name\": \"A\", \"method\": \"GET\", \"url\": \"u\", \"params\": [{\"name\": \"a\", \"value\": \"1\"}, // second\n {\"name\": \"b\", \"value\": \"2\"}]}",
			change: func(r *Request) { r.Params = r.Params[:1] },
			want:   "{\n  \"emetosHttp\": 1,\n  \"name\": \"A\",\n  \"method\": \"GET\",\n  \"url\": \"u\",\n  \"params\": [\n    {\n      \"name\": \"a\",\n      \"value\": \"1\"\n    }\n  ]\n}",
		},
		{
			name: "a body type change replaces the body object, a setting appears in order",
			text: "{\"emetosHttp\": 1, \"name\": \"A\", \"method\": \"POST\", \"url\": \"u\", \"body\": {\"type\": \"json\", \"file\": \"a.body.json\"}}",
			change: func(r *Request) {
				r.Body = &Body{Type: BodyXML, File: "a.body.xml"}
				r.Settings = &Settings{TimeoutMs: ptr(5)}
			},
			want: "{\n  \"emetosHttp\": 1,\n  \"name\": \"A\",\n  \"method\": \"POST\",\n  \"url\": \"u\",\n  \"body\": {\n    \"type\": \"xml\",\n    \"file\": \"a.body.xml\"\n  },\n  \"settings\": {\n    \"timeoutMs\": 5\n  }\n}",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := Parse[Request](tt.text)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if tt.change != nil {
				tt.change(&r)
			}
			got, err := Update(tt.text, r)
			if err != nil {
				t.Fatalf("Update: %v", err)
			}
			if got != tt.want {
				t.Errorf("Update =\n%q\nwant\n%q", got, tt.want)
			}
			// what was written reads back as the document
			back, err := Parse[Request](got)
			if err != nil {
				t.Fatalf("Parse(Update): %v", err)
			}
			if again, err := Update(got, back); err != nil || again != got {
				t.Errorf("a second Update changed the text (err %v):\n%q", err, again)
			}
		})
	}
}

func TestUpdateOfACollectionAndAnEnvironment(t *testing.T) {
	text := "// the API\n{\n  \"emetosHttp\": 1,\n  \"name\": \"API\",\n  \"variables\": [\n    {\"name\": \"base\", \"value\": \"http://a\"}, // dev\n    {\"name\": \"tok\", \"value\": \"s3cret\", \"secret\": true}\n  ],\n  \"x\": 1\n}\n"
	c, err := Parse[Collection](text)
	if err != nil {
		t.Fatal(err)
	}
	c.Order = []string{"a.request.jsonc"}
	c.Variables[0].Value = "http://b"
	got, err := Update(text, c)
	if err != nil {
		t.Fatal(err)
	}
	want := "// the API\n{\n  \"emetosHttp\": 1,\n  \"name\": \"API\",\n  \"order\": [\n    \"a.request.jsonc\"\n  ],\n  \"variables\": [\n    {\"name\": \"base\", \"value\": \"http://b\"}, // dev\n    {\n      \"name\": \"tok\",\n      \"secret\": true\n    }\n  ],\n  \"x\": 1\n}\n"
	if got != want {
		t.Errorf("Update =\n%q\nwant\n%q", got, want)
	}
	for _, leak := range []string{"s3cret"} {
		if contains(got, leak) {
			t.Errorf("the secret value %q is still in the text", leak)
		}
	}
	env, err := Parse[Environment]("{\"emetosHttp\": 1, \"name\": \"Dev\"}\n")
	if err != nil {
		t.Fatal(err)
	}
	env.Variables = []Variable{{Name: "a", Value: "1", Enabled: true}}
	got, err = Update("{\"emetosHttp\": 1, \"name\": \"Dev\"}\n", env)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Parse[Environment](got); err != nil {
		t.Errorf("the updated environment does not read: %v\n%s", err, got)
	}
}

func TestUpdateOfAFileThatIsNotOneOfTheClient(t *testing.T) {
	tests := []struct {
		name, text, tag string
	}{
		{"broken", "{\"a\" 1}", "HttpFileSyntax"},
		{"an array", "[]", "HttpFileNotObject"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Update(tt.text, Request{Name: "x", Method: "GET"})
			wantRoute(t, err, "Update")
			wantRoute(t, err, tt.tag)
		})
	}
}

func TestUpdateOfAFileWithoutAVersionAddsIt(t *testing.T) {
	got, err := Update("{\"name\": \"A\", \"method\": \"GET\", \"url\": \"u\"}", Request{Name: "A", Method: "GET", URL: "u"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Parse[Request](got); err != nil {
		t.Errorf("%v\n%s", err, got)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestUpdateReplacesAnElementOfAList(t *testing.T) {
	text := "{\n  \"emetosHttp\": 1,\n  \"name\": \"C\",\n  \"order\": [\n    \"a\", // first\n    \"b\",\n    \"c\"\n  ]\n}\n"
	c, err := Parse[Collection](text)
	if err != nil {
		t.Fatal(err)
	}
	c.Order = []string{"a", "x", "c"}
	got, err := Update(text, c)
	if err != nil {
		t.Fatal(err)
	}
	back, err := Parse[Collection](got)
	if err != nil || len(back.Order) != 3 || back.Order[1] != "x" {
		t.Errorf("order = %v (%v)\n%s", back.Order, err, got)
	}
	if !contains(got, "// first") {
		t.Errorf("the comment is gone:\n%s", got)
	}
}
