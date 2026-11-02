package typescript

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

// What the TypeScript pack adds to the debugger of the JavaScript pack (step 25.10, SPEC §10.31): the outFiles of a TypeScript program.

func TestTheOutFilesOfATypeScriptProgramComeFromItsTsconfig(t *testing.T) {
	const out = `{"compilerOptions": {"outDir": "out"}}`
	tests := []struct {
		name  string
		files map[string]string
		in    extension.DebugConfiguration
		want  any // the outFiles, with @folder for the folder
	}{
		{"the outDir of the project", map[string]string{"tsconfig.json": out},
			extension.DebugConfiguration{"request": "launch", "program": "@folder/src/app.ts"}, []any{"@folder/out/**/*.js", "!**/node_modules/**"}},
		{"a ./ and comments in the tsconfig", map[string]string{"tsconfig.json": "{\n// c\n\"compilerOptions\": {\"outDir\": \"./build/\",},\n}"},
			extension.DebugConfiguration{"request": "launch", "program": "@folder/app.mts"}, []any{"@folder/build/**/*.js", "!**/node_modules/**"}},
		{"the tsconfig nearest to the program", map[string]string{"tsconfig.json": out, "pkg/tsconfig.json": `{"compilerOptions": {"outDir": "dist"}}`},
			extension.DebugConfiguration{"request": "launch", "program": "@folder/pkg/src/a.tsx"}, []any{"@folder/pkg/dist/**/*.js", "!**/node_modules/**"}},
		{"an outDir that is absolute", map[string]string{"tsconfig.json": `{"compilerOptions": {"outDir": "/elsewhere/out"}}`},
			extension.DebugConfiguration{"request": "launch", "program": "@folder/a.cts"}, []any{"/elsewhere/out/**/*.js", "!**/node_modules/**"}},
		{"a program relative to the cwd", map[string]string{"tsconfig.json": out},
			extension.DebugConfiguration{"request": "launch", "program": "src/app.ts", "cwd": "@folder"}, []any{"@folder/out/**/*.js", "!**/node_modules/**"}},
		{"outFiles the user wrote", map[string]string{"tsconfig.json": out},
			extension.DebugConfiguration{"request": "launch", "program": "@folder/a.ts", "outFiles": []any{"x/**"}}, []any{"x/**"}},
		{"source maps turned off", map[string]string{"tsconfig.json": out},
			extension.DebugConfiguration{"request": "launch", "program": "@folder/a.ts", "sourceMaps": false}, nil},
		{"a JavaScript program", map[string]string{"tsconfig.json": out},
			extension.DebugConfiguration{"request": "launch", "program": "@folder/a.js"}, nil},
		{"a declaration file", map[string]string{"tsconfig.json": out},
			extension.DebugConfiguration{"request": "launch", "program": "@folder/a.d.ts"}, nil},
		{"an attach", map[string]string{"tsconfig.json": out}, extension.DebugConfiguration{"request": "attach", "port": float64(9229)}, nil},
		{"a child session", map[string]string{"tsconfig.json": out},
			extension.DebugConfiguration{"request": "launch", "program": "@folder/a.ts", "__pendingTargetId": "t1"}, nil},
		{"no tsconfig", nil, extension.DebugConfiguration{"request": "launch", "program": "@folder/a.ts"}, nil},
		{"a tsconfig with no outDir", map[string]string{"tsconfig.json": `{"compilerOptions": {"strict": true}}`},
			extension.DebugConfiguration{"request": "launch", "program": "@folder/a.ts"}, nil},
		{"no program", map[string]string{"tsconfig.json": out}, extension.DebugConfiguration{"request": "launch"}, nil},
	}
	for _, tt := range tests {
		for _, typ := range []string{"node", "pwa-node"} {
			t.Run(tt.name+" ("+typ+")", func(t *testing.T) {
				r := newRig(t, rigOptions{files: tt.files})
				subst := func(v any) any {
					switch x := v.(type) {
					case string:
						return strings.ReplaceAll(x, "@folder", r.folder)
					case []any:
						out := make([]any, len(x))
						for i, e := range x {
							out[i] = e
							if s, ok := e.(string); ok {
								out[i] = strings.ReplaceAll(s, "@folder", r.folder)
							}
						}
						return out
					}
					return v
				}
				in := extension.DebugConfiguration{}
				for k, v := range tt.in {
					in[k] = subst(v)
				}
				got, err := r.api.ResolveDebugConfigurationSubstituted(context.Background(), typ, r.folder, in)
				if err != nil || got == nil {
					t.Fatalf("resolved to %v, %v", got, err)
				}
				if want := subst(tt.want); !reflect.DeepEqual(got["outFiles"], want) {
					t.Errorf("outFiles = %#v, want %#v", got["outFiles"], want)
				}
				// the configuration given is not changed
				if _, had := tt.in["outFiles"]; !had {
					if _, has := in["outFiles"]; has {
						t.Error("the configuration that was given was changed")
					}
				}
			})
		}
	}
}

func TestThePackAddsNothingToTheDebuggerOfAHostWithoutOne(t *testing.T) {
	r := newRigWith(t, rigOptions{}, func(api *fakeapi.API) { api.SetDebugUnavailable(true) }) // the activation does not fail
	got, err := r.api.ResolveDebugConfigurationSubstituted(context.Background(), "node", r.folder, extension.DebugConfiguration{"request": "launch"})
	if err != nil || got["request"] != "launch" {
		t.Errorf("resolved to %v, %v", got, err)
	}
}
