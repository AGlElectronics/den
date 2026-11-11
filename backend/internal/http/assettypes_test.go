package http

import (
	"net/http/httptest"
	"testing"
)

func TestDetectAssetType(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")
	jpeg := []byte("\xff\xd8\xff\xe0\x00\x10JFIF\x00")
	webp := []byte("RIFF\x24\x00\x00\x00WEBPVP8 ")
	ico := []byte("\x00\x00\x01\x00\x01\x00\x10\x10")
	svg := []byte("\xef\xbb\xbf  <svg xmlns=\"http://www.w3.org/2000/svg\"></svg>")
	cases := []struct {
		name, file, declared string
		head                 []byte
		want                 string
	}{
		{"png", "a.png", "image/png", png, "image/png"},
		{"png with wrong header", "a.txt", "text/plain", png, "image/png"},
		{"jpeg", "a.jpg", "", jpeg, "image/jpeg"},
		{"webp", "a.webp", "", webp, "image/webp"},
		{"ico", "a.ico", "", ico, "image/x-icon"},
		{"svg by name", "a.svg", "", svg, "image/svg+xml"},
		{"svg by type", "a", "image/svg+xml", svg, "image/svg+xml"},
		{"svg without svg name", "a.xml", "text/xml", svg, ""},
		{"html named svg", "a.svg", "image/svg+xml", []byte("<html><body></body></html>"), ""},
		{"html", "a.html", "text/html", []byte("<!doctype html><html>"), ""},
		{"gif", "a.gif", "image/gif", []byte("GIF89a"), ""},
		{"pdf", "a.pdf", "application/pdf", []byte("%PDF-1.7"), ""},
	}
	for _, c := range cases {
		if got := detectAssetType(c.head, c.file, c.declared); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestSetAssetHeadersUnknownType(t *testing.T) {
	rec := httptest.NewRecorder()
	setAssetHeaders(rec, "text/html")
	if rec.Header().Get("Content-Type") != "application/octet-stream" || rec.Header().Get("Content-Disposition") != "attachment" ||
		rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("headers: %v", rec.Header())
	}
}
