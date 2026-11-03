package httpclient

import (
	"context"
	"io"
	"mime"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/httpclient/engine"
)

// The actions of the response pane: Save Response to File, Copy Body and Open in New Editor Tab.

// responseFileName is the name the Save dialog starts with: the file name of the Content-Disposition header, else the last part of the
// URL's path, else "response", with an extension that fits the kind of the body when the name has none.
func responseFileName(resp *engine.Response, disp *respDisplay) string {
	name := ""
	if _, params, err := mime.ParseMediaType(resp.Header.Get("Content-Disposition")); err == nil {
		name = filepath.Base(params["filename"])
	}
	if name == "" || name == "." || name == string(filepath.Separator) {
		name = ""
		if u, err := url.Parse(resp.URL); err == nil {
			name = path.Base(u.Path)
		}
	}
	if name == "" || name == "." || name == "/" {
		name = "response"
	}
	if path.Ext(name) == "" {
		name += map[string]string{
			bodyKindJSON: ".json", bodyKindXML: ".xml", bodyKindHTML: ".html", bodyKindJS: ".js", bodyKindCSS: ".css", bodyKindText: ".txt",
		}[disp.kind]
		if disp.kind == bodyKindImage {
			if exts, err := mime.ExtensionsByType(disp.mediaType); err == nil && len(exts) > 0 {
				name += exts[len(exts)-1]
			}
		}
	}
	return name
}

// ctxReader stops a copy when its context ends.
type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err //nolint:wrapcheck // the caller of the copy wraps it
	}
	return c.r.Read(p) //nolint:wrapcheck // the caller of the copy wraps it
}

// writeBody writes the body, as it was received (decoded, not formatted), to a file; a file that exists is replaced. It runs off the UI
// thread.
func writeBody(ctx context.Context, body *engine.Body, to string) error {
	src, err := body.Open()
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()
	f, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return errors.NewErr("HttpResponseFileCreateFailed", err).SetMetadata("path", to)
	}
	_, err = io.Copy(f, ctxReader{ctx: ctx, r: src})
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return errors.NewErr("HttpResponseFileWriteFailed", err).SetMetadata("path", to)
	}
	return nil
}

// saveResponse asks where to write the body of the response and writes it.
func (t *reqTab) saveResponse() {
	r := t.send.resp
	if r == nil || r.Body == nil || t.pane == nil {
		return
	}
	e, body, name := t.e, r.Body, responseFileName(r, t.pane.disp)
	var saved string
	e.async(func(ctx context.Context) error {
		to, err := e.api.FileChooser().Save(ctx, extension.FileChooserOptions{Title: "Save Response", AcceptLabel: "Save", Name: name, Folder: e.folder})
		if err != nil || to == "" {
			return err //nolint:wrapcheck // the API's own error
		}
		if err := writeBody(ctx, body, to); err != nil {
			return err
		}
		saved = to
		return nil
	}, func(err error) {
		switch {
		case err != nil:
			e.fail("save the response", err)
		case saved != "":
			e.api.Notifications().Info("Saved the response (" + byteSize(body.Size()) + ") to " + saved)
		}
	})
}

// shownText is the text of the Body tab as it is shown: the Pretty text when that is what is on screen, else the whole body. It refuses
// a body that is not text and one larger than maxCopy.
func (t *reqTab) shownText() (text string, ok bool) {
	r, p := t.send.resp, t.pane
	if r == nil || r.Body == nil || p == nil || !p.disp.textual() {
		return "", false
	}
	if p.mode == modePretty && p.pretty != nil {
		return strings.Join(p.disp.pretty, "\n") + "\n", true
	}
	if r.Body.Size() > maxCopy {
		t.e.api.Notifications().Warn("The body is " + byteSize(r.Body.Size()) + ", too large to copy or open here. Use Save Response to File.")
		return "", false
	}
	data, err := r.Body.Bytes()
	if err != nil {
		t.e.fail("read the response", err)
		return "", false
	}
	return strings.ToValidUTF8(string(data), "�"), true
}

// copyBody puts the body on the clipboard.
func (t *reqTab) copyBody() {
	text, ok := t.shownText()
	if !ok {
		return
	}
	if err := t.e.api.Clipboard().WriteText(text); err != nil {
		t.e.fail("copy the body", err)
		return
	}
	t.e.api.Notifications().Info("Copied the body (" + byteSize(int64(len(text))) + ").")
}

// openBodyInTab opens the body in a new, untitled editor tab in the language of its kind.
func (t *reqTab) openBodyInTab() {
	text, ok := t.shownText()
	if !ok {
		return
	}
	lang := ""
	if name := t.pane.disp.lang; name != "" {
		lang, _ = t.e.api.Languages().Resolve(name)
	}
	if _, err := t.e.api.Documents().OpenUntitled(lang, text); err != nil {
		t.e.fail("open the body in an editor", err)
	}
}
