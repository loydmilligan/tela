// Package extract turns a stored file's bytes into a plain-text representation
// for the document RAG index + summaries. It is intentionally pure-Go: the
// runtime is distroless (no pdftotext/libreoffice in the image), so PDF text is
// read from the text layer in-process — no OCR (a scanned PDF yields nothing).
// Office formats (docx/pptx/xlsx) are a later tier via the gotenberg sidecar.
package extract

import (
	"bytes"
	"io"
	"strings"

	"github.com/ledongthuc/pdf"
)

// Text returns a file's extractable text. ok=false means "not text-extractable"
// (an image, a binary, an encrypted/scanned PDF, or an extraction failure) —
// callers treat that as "don't index", never an error. Best-effort by design.
func Text(mime, name string, data []byte) (text string, ok bool) {
	switch {
	case isPlainText(mime, name):
		return string(data), true
	case mime == "application/pdf" || lowerExt(name) == "pdf":
		return pdfText(data)
	default:
		return "", false
	}
}

func isPlainText(mime, name string) bool {
	if strings.HasPrefix(mime, "text/") {
		return true
	}
	switch mime {
	case "application/json", "application/yaml", "application/x-yaml", "application/xml":
		return true
	}
	switch lowerExt(name) {
	case "txt", "md", "markdown", "csv", "tsv", "json", "yaml", "yml", "xml", "log", "rst", "org":
		return true
	}
	return false
}

func lowerExt(name string) string {
	i := strings.LastIndexByte(name, '.')
	if i < 0 || i == len(name)-1 {
		return ""
	}
	return strings.ToLower(name[i+1:])
}

// pdfText extracts a PDF's text layer (no OCR). ledongthuc/pdf can panic on
// malformed input, so it's guarded — a bad PDF is "not indexable", not a crash.
func pdfText(data []byte) (text string, ok bool) {
	defer func() {
		if recover() != nil {
			text, ok = "", false
		}
	}()
	r, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", false
	}
	rc, err := r.GetPlainText()
	if err != nil {
		return "", false
	}
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, rc); err != nil {
		return "", false
	}
	s := strings.TrimSpace(buf.String())
	if s == "" {
		return "", false // scanned PDF / no text layer
	}
	return s, true
}
