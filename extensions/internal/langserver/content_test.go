package langserver

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

func TestProvideContentRegistersTheProviderOrSaysNothingWhereDocumentsCannotBeShown(t *testing.T) {
	p := extension.ContentProvider{Scheme: "jdt", Text: func(context.Context, string) (string, error) { return "text", nil }}
	tests := []struct {
		name     string
		fail     error
		wantReg  bool
		wantLog  string
		wantWarn bool
	}{
		{"a scope with documents", nil, true, "", false},
		{"a scope that cannot show documents", errors.New("ExtensionDocumentUnavailable", "no documents"), false, "the class files are not available in this scope", false},
		{"another failure", errors.New("ExtensionContentSchemeInvalid", "bad scheme"), false, "the class files are not available", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			api := fakeapi.New(t, extension.Manifest{ID: "emetos.x", Name: "X", Version: "0.1.0", ActivationEvents: []string{extension.OnStartup}}, extension.ScopeInfo{Name: "tab"})
			api.SetLogger(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
			api.FailProvideContent(tt.fail)
			reg := ProvideContent(api, "the class files", p)
			if (reg != nil) != tt.wantReg {
				t.Errorf("registration = %v", reg)
			}
			if tt.wantLog != "" && !strings.Contains(buf.String(), tt.wantLog) {
				t.Errorf("log %q lacks %q", buf.String(), tt.wantLog)
			}
			if strings.Contains(buf.String(), "level=WARN") != tt.wantWarn {
				t.Errorf("log %q: warning = %v, want %v", buf.String(), strings.Contains(buf.String(), "level=WARN"), tt.wantWarn)
			}
			if tt.wantReg {
				if text, err := api.ContentText(context.Background(), extension.ContentURI("jdt", "a")); err != nil || text != "text" {
					t.Errorf("text = %q, %v", text, err)
				}
			}
		})
	}
}
