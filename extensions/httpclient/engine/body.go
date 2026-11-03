package engine

import (
	"bytes"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/httpclient/format"
)

// bodySource makes the body of a request, as often as the request is sent (a redirect that keeps the body, a Digest answer).
type bodySource struct {
	contentType string
	// open returns the body and its length, -1 when it is not known.
	open func() (io.ReadCloser, int64, error)
}

func bytesSource(contentType string, data []byte) *bodySource {
	return &bodySource{contentType: contentType, open: func() (io.ReadCloser, int64, error) {
		return io.NopCloser(bytes.NewReader(data)), int64(len(data)), nil
	}}
}

// readBodyFile reads a body file named beside the request (the text of a raw body, the variables of a GraphQL body).
func readBodyFile(in Input, name string) (string, error) {
	if name == "" {
		return "", nil
	}
	if text, ok := in.Texts[name]; ok {
		return text, nil
	}
	dir := in.Dir
	if dir == "" {
		dir = in.Folder
	}
	full, err := format.ResolvePath(in.Folder, filepath.Join(dir, name))
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return "", errors.NewErr("EngineBodyFileReadFailed", err).SetMetadata("path", full)
	}
	return string(data), nil
}

// buildBody makes the body source of the request's body, and sets the Content-Type header unless the request has one.
func buildBody(in Input, resolve func(string) string, header http.Header) (*bodySource, error) {
	b := in.Request.Body
	if b == nil || b.Type == "" || b.Type == format.BodyNone {
		return nil, nil
	}
	var src *bodySource
	switch b.Type {
	case format.BodyJSON, format.BodyXML, format.BodyText, format.BodyHTML, format.BodyJavaScript:
		text, err := readBodyFile(in, b.File)
		if err != nil {
			return nil, err
		}
		src = bytesSource(rawContentType(b.Type), []byte(resolve(text)))
	case format.BodyGraphQL:
		query, err := readBodyFile(in, b.File)
		if err != nil {
			return nil, err
		}
		vars, err := readBodyFile(in, b.Variables)
		if err != nil {
			return nil, err
		}
		doc := map[string]any{"query": resolve(query)}
		if v := strings.TrimSpace(resolve(vars)); v != "" {
			if !json.Valid([]byte(v)) {
				return nil, errors.New("EngineGraphQLVariablesInvalid", "the variables of the GraphQL body are not JSON", "file", b.Variables)
			}
			doc["variables"] = json.RawMessage(v)
		}
		data, err := json.Marshal(doc)
		if err != nil {
			return nil, errors.NewErr("EngineBodyInvalid", err).SetMetadata("type", b.Type)
		}
		src = bytesSource("application/json", data)
	case format.BodyForm:
		var parts []string
		for _, f := range b.Fields {
			if f.Enabled && f.Name != "" {
				parts = append(parts, url.QueryEscape(resolve(f.Name))+"="+url.QueryEscape(resolve(f.Value)))
			}
		}
		src = bytesSource("application/x-www-form-urlencoded", []byte(strings.Join(parts, "&")))
	case format.BodyMultipart:
		var err error
		if src, err = multipartSource(in, resolve); err != nil {
			return nil, err
		}
	case format.BodyBinary:
		if b.Path == "" {
			return nil, errors.New("EngineBodyFileReadFailed", "the binary body names no file")
		}
		full, err := format.ResolvePath(in.Folder, resolve(b.Path))
		if err != nil {
			return nil, err
		}
		st, err := os.Stat(full)
		if err != nil {
			return nil, errors.NewErr("EngineBodyFileReadFailed", err).SetMetadata("path", full)
		}
		if st.IsDir() {
			return nil, errors.New("EngineBodyFileReadFailed", "the binary body is a folder", "path", full)
		}
		src = &bodySource{contentType: "application/octet-stream", open: func() (io.ReadCloser, int64, error) {
			f, err := os.Open(full)
			if err != nil {
				return nil, 0, errors.NewErr("EngineBodyFileReadFailed", err).SetMetadata("path", full)
			}
			return f, st.Size(), nil
		}}
	default:
		return nil, errors.New("EngineBodyInvalid", "the body type is not known", "type", b.Type)
	}
	if b.Type == format.BodyMultipart || header.Get("Content-Type") == "" {
		header.Set("Content-Type", src.contentType)
	}
	return src, nil
}

func rawContentType(t string) string {
	switch t {
	case format.BodyJSON:
		return "application/json"
	case format.BodyXML:
		return "application/xml"
	case format.BodyHTML:
		return "text/html"
	case format.BodyJavaScript:
		return "application/javascript"
	}
	return "text/plain"
}

// multipartPart is one field of a multipart body after the variables were replaced.
type multipartPart struct {
	name, value, path string
}

// multipartSource streams a multipart/form-data body through a pipe: a file is never read whole into memory.
func multipartSource(in Input, resolve func(string) string) (*bodySource, error) {
	var parts []multipartPart
	for _, f := range in.Request.Body.Fields {
		if !f.Enabled || f.Name == "" {
			continue
		}
		part := multipartPart{name: resolve(f.Name), value: resolve(f.Value)}
		if f.File != "" {
			full, err := format.ResolvePath(in.Folder, resolve(f.File))
			if err != nil {
				return nil, err
			}
			st, err := os.Stat(full)
			if err != nil {
				return nil, errors.NewErr("EngineMultipartFileFailed", err).SetMetadata("field", part.name, "path", full)
			}
			if st.IsDir() {
				return nil, errors.New("EngineMultipartFileFailed", "the file of the field is a folder", "field", part.name, "path", full)
			}
			part.path = full
		}
		parts = append(parts, part)
	}
	boundary := multipart.NewWriter(io.Discard).Boundary()
	return &bodySource{
		contentType: "multipart/form-data; boundary=" + boundary,
		open: func() (io.ReadCloser, int64, error) {
			pr, pw := io.Pipe()
			go func() { pw.CloseWithError(writeMultipart(pw, boundary, parts)) }()
			return pr, -1, nil
		},
	}, nil
}

func writeMultipart(w io.Writer, boundary string, parts []multipartPart) error {
	mw := multipart.NewWriter(w)
	if err := mw.SetBoundary(boundary); err != nil {
		return errors.NewErr("EngineMultipartWriteFailed", err)
	}
	for _, p := range parts {
		if p.path == "" {
			if err := mw.WriteField(p.name, p.value); err != nil {
				return errors.NewErr("EngineMultipartWriteFailed", err).SetMetadata("field", p.name)
			}
			continue
		}
		h := textproto.MIMEHeader{}
		h.Set("Content-Disposition", mime.FormatMediaType("form-data", map[string]string{"name": p.name, "filename": filepath.Base(p.path)}))
		ct := mime.TypeByExtension(filepath.Ext(p.path))
		if ct == "" {
			ct = "application/octet-stream"
		}
		h.Set("Content-Type", ct)
		pw, err := mw.CreatePart(h)
		if err != nil {
			return errors.NewErr("EngineMultipartWriteFailed", err).SetMetadata("field", p.name)
		}
		f, err := os.Open(p.path)
		if err != nil {
			return errors.NewErr("EngineMultipartFileFailed", err).SetMetadata("field", p.name, "path", p.path)
		}
		_, err = io.Copy(pw, f)
		_ = f.Close()
		if err != nil {
			return errors.NewErr("EngineMultipartWriteFailed", err).SetMetadata("field", p.name)
		}
	}
	if err := mw.Close(); err != nil {
		return errors.NewErr("EngineMultipartWriteFailed", err)
	}
	return nil
}
