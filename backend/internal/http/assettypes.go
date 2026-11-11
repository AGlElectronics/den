package http

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"path"
	"strings"
)

// AssetStore is the object storage branding assets live in (Den's bucket);
// *files.Client implements it.
type AssetStore interface {
	PutObject(ctx context.Context, key string, r io.Reader, size int64, contentType string) error
	GetObject(ctx context.Context, key string) (io.ReadCloser, error)
	Remove(ctx context.Context, key string) error
	Bucket() string
}

// Branding asset roles Den accepts; the newest upload per role is in use.
var assetRoles = map[string]bool{"logo": true, "favicon": true, assetLoginBackground: true}

// allowedAssetTypes are the content types Den stores and serves for
// branding assets.
var allowedAssetTypes = map[string]bool{
	"image/png":     true,
	"image/jpeg":    true,
	"image/webp":    true,
	"image/x-icon":  true,
	"image/svg+xml": true,
}

const assetTypeSVG = "image/svg+xml"

// assetExtension names stored objects after their detected type, never the
// client's file name.
var assetExtension = map[string]string{
	"image/png": ".png", "image/jpeg": ".jpg", "image/webp": ".webp", "image/x-icon": ".ico", assetTypeSVG: ".svg",
}

// detectAssetType works out the content type from the file's first bytes
// (never from the client's header). SVG is text, so it is recognised by
// its extension or declared type plus an <svg element near the start.
// It returns "" for anything Den does not accept.
func detectAssetType(head []byte, filename, declared string) string {
	sniffed := http.DetectContentType(head)
	switch sniffed {
	case "image/png", "image/jpeg", "image/webp":
		return sniffed
	case "image/x-icon", "image/vnd.microsoft.icon":
		return "image/x-icon"
	}
	isSVGName := strings.EqualFold(path.Ext(filename), ".svg") || strings.HasPrefix(strings.ToLower(declared), assetTypeSVG)
	if isSVGName && (strings.HasPrefix(sniffed, "text/xml") || strings.HasPrefix(sniffed, "text/plain")) {
		trimmed := bytes.TrimLeft(bytes.TrimPrefix(head, []byte("\xef\xbb\xbf")), " \t\r\n")
		if bytes.HasPrefix(trimmed, []byte("<")) && bytes.Contains(bytes.ToLower(head), []byte("<svg")) {
			return assetTypeSVG
		}
	}
	return ""
}

// setAssetHeaders sets the headers every served branding asset gets. SVG
// can carry scripts, so it is served as a download with a sandbox policy:
// <img> tags still show it, opening it directly runs nothing. Types outside
// the whitelist (rows from before the whitelist) are served as opaque
// downloads.
func setAssetHeaders(w http.ResponseWriter, contentType string) {
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	switch {
	case contentType == assetTypeSVG:
		h.Set("Content-Type", assetTypeSVG)
		h.Set("Content-Disposition", "attachment")
		h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	case allowedAssetTypes[contentType]:
		h.Set("Content-Type", contentType)
	default:
		h.Set("Content-Type", "application/octet-stream")
		h.Set("Content-Disposition", "attachment")
	}
}
