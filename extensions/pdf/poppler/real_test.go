//go:build emetos_poppler

package poppler

import (
	"bytes"
	"crypto/md5"
	"crypto/rc4"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"testing"
)

// The client against the real Poppler (go test -tags emetos_poppler ./extensions/pdf/poppler): PDFs written by hand in the test, one
// page of text and a red rectangle, and the same page locked with the user password "user" (the standard security handler, RC4 with
// 40-bit keys).

func realPrograms(t *testing.T) Programs {
	t.Helper()
	p, err := Find(t.Context(), FindOptions{Path: os.Getenv("PATH")})
	if err != nil {
		t.Skipf("no usable Poppler: %v", err)
	}
	return p
}

// pad is the padding of the standard security handler.
var pad, _ = hex.DecodeString("28BF4E5E4E758A4164004E56FFFA01082E2E00B6D0683E802F0CA9FE6453697A")

func padded(pw string) []byte { return append([]byte(pw), pad...)[:32] }

func rc4Of(t *testing.T, key, data []byte) []byte {
	c, err := rc4.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]byte, len(data))
	c.XORKeyStream(out, data)
	return out
}

// writePDF writes a PDF of one page, 612 x 792 points, with "Hello PDF" and a red rectangle at (72, 400, 200 x 100); a password
// encrypts it.
func writePDF(t *testing.T, password string) string {
	t.Helper()
	content := []byte("BT /F1 24 Tf 72 700 Td (Hello PDF) Tj ET 1 0 0 rg 72 400 200 100 re f")
	id := []byte("0123456789abcdef")
	var encrypt string
	if password != "" {
		ownerKey := md5.Sum(padded("owner"))
		o := rc4Of(t, ownerKey[:5], padded(password))
		p := make([]byte, 4)
		binary.LittleEndian.PutUint32(p, uint32(0xFFFFFFFC)) // P = -4
		sum := md5.Sum(bytes.Join([][]byte{padded(password), o, p, id}, nil))
		key := sum[:5]
		u := rc4Of(t, key, pad)
		objKey := md5.Sum(append(append([]byte{}, key...), 4, 0, 0, 0, 0)) // object 4, generation 0
		content = rc4Of(t, objKey[:10], content)
		encrypt = fmt.Sprintf("<< /Filter /Standard /V 1 /R 2 /O <%x> /U <%x> /P -4 >>", o, u)
	}
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}
	if encrypt != "" {
		objs = append(objs, encrypt)
	}
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objs))
	for i, o := range objs {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, o := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", o)
	}
	trailer := fmt.Sprintf("/Size %d /Root 1 0 R /ID [<%x> <%x>]", len(objs)+1, id, id)
	if encrypt != "" {
		trailer += fmt.Sprintf(" /Encrypt %d 0 R", len(objs))
	}
	fmt.Fprintf(&b, "trailer\n<< %s >>\nstartxref\n%d\n%%%%EOF\n", trailer, xref)
	path := filepath.Join(t.TempDir(), "a.pdf")
	if err := os.WriteFile(path, b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// red tells whether the pixel is red.
func red(img image.Image, x, y int) bool {
	r, g, b, _ := img.At(x, y).RGBA()
	return r>>8 > 200 && g>>8 < 60 && b>>8 < 60
}

func TestTheRealPoppler(t *testing.T) {
	p := realPrograms(t)
	t.Logf("Poppler %s in %s", p.Version, p.Dir)
	for _, password := range []string{"", "user"} {
		t.Run("password "+password, func(t *testing.T) {
			file := writePDF(t, password)
			doc, err := Info(t.Context(), p, file, password)
			if err != nil {
				t.Fatal(err)
			}
			if len(doc.Pages) != 1 || doc.Pages[0] != (Page{612, 792}) || doc.Encrypted != (password != "") {
				t.Errorf("got %+v", doc)
			}
			img, err := Render(t.Context(), p, file, password, 1, 36)
			if err != nil {
				t.Fatal(err)
			}
			if img.Bounds() != image.Rect(0, 0, 306, 396) {
				t.Errorf("the page is %v, want 306 x 396", img.Bounds())
			}
			// the rectangle: x 72..272, y 400..500 from the bottom, so 36..136 and 146..196 from the top at half size
			if !red(img, 80, 170) || red(img, 20, 20) {
				t.Error("the red rectangle is not where it was drawn")
			}
			if password == "" {
				return
			}
			_, err = Info(t.Context(), p, file, "")
			route(t, err, "Info.PDFPasswordNeeded")
			_, err = Info(t.Context(), p, file, "guess")
			route(t, err, "Info.PDFPasswordWrong")
			_, err = Render(t.Context(), p, file, "", 1, 36)
			route(t, err, "Render.PDFPasswordNeeded")
		})
	}
	t.Run("a damaged file", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "bad.pdf")
		if err := os.WriteFile(file, []byte("%PDF-1.4\ngarbage\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := Info(t.Context(), p, file, "")
		route(t, err, "Info.PDFDamaged")
	})
}
