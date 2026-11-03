package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rm4n0s/errors"
)

// failWriter fails on its nth Write.
type failWriter struct{ n int } // the call (counted from 1) that fails

func (w *failWriter) Write(p []byte) (int, error) {
	w.n--
	if w.n == 0 {
		return 0, errors.New("PipeClosed", "the pipe is closed")
	}
	return len(p), nil
}

func TestWriteMultipartFailures(t *testing.T) {
	file := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(file, []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name     string
		boundary string
		parts    []multipartPart
		limit    int
		route    string
	}{
		{name: "a boundary that is not valid", boundary: "", route: "writeMultipart.EngineMultipartWriteFailed"},
		{name: "a field that cannot be written", boundary: "b", parts: []multipartPart{{name: "a", value: "1"}}, limit: 1, route: "writeMultipart.EngineMultipartWriteFailed"},
		{name: "a file part that cannot be started", boundary: "b", parts: []multipartPart{{name: "a", path: file}}, limit: 1, route: "writeMultipart.EngineMultipartWriteFailed"},
		{name: "a file that is gone", boundary: "b", parts: []multipartPart{{name: "a", path: file + ".gone"}}, limit: 99, route: "writeMultipart.EngineMultipartFileFailed"},
		{name: "a file that cannot be copied", boundary: "b", parts: []multipartPart{{name: "a", path: file}}, limit: 2, route: "writeMultipart.EngineMultipartWriteFailed"},
		{name: "the closing line", boundary: "b", parts: []multipartPart{{name: "a", value: "1"}}, limit: 3, route: "writeMultipart.EngineMultipartWriteFailed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := writeMultipart(&failWriter{n: tt.limit}, tt.boundary, tt.parts)
			wantRoute(t, err, tt.route)
		})
	}
}
