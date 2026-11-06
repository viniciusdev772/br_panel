package server

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
)

// asset is one embedded static file, ready to serve: the embedded filesystem has no modification
// times, so without this every page load would download the CSS and JS again, uncompressed.
type asset struct {
	body, gz []byte // gz is nil for formats that do not compress
	ctype    string
	ver      string // content hash: part of the URL (?v=) and of the ETag
}

type assets map[string]asset

var staticTypes = map[string]string{
	".css": "text/css; charset=utf-8", ".js": "text/javascript; charset=utf-8", ".svg": "image/svg+xml",
	".json": "application/json", ".png": "image/png", ".ico": "image/x-icon", ".woff2": "font/woff2",
}

func loadAssets(fsys fs.FS) (assets, error) {
	out := assets{}
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(b)
		a := asset{body: b, ver: hex.EncodeToString(sum[:6])}
		ext := path.Ext(p)
		if a.ctype = staticTypes[ext]; a.ctype == "" {
			if a.ctype = mime.TypeByExtension(ext); a.ctype == "" {
				a.ctype = "application/octet-stream"
			}
		}
		if strings.HasPrefix(a.ctype, "text/") || ext == ".svg" || ext == ".json" {
			var buf bytes.Buffer
			zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
			_, _ = zw.Write(b)
			if zw.Close() == nil && buf.Len() < len(b) {
				a.gz = buf.Bytes()
			}
		}
		out[p] = a
		return nil
	})
	return out, err
}

// url is the address pages use: the version in the query makes it safe to cache forever.
func (a assets) url(name string) string {
	return "/static/" + name + "?v=" + a[name].ver
}

func (a assets) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f, ok := a[strings.TrimPrefix(r.URL.Path, "/static/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		h := w.Header()
		h.Set("Content-Type", f.ctype)
		h.Set("Vary", "Accept-Encoding")
		h.Set("ETag", `"`+f.ver+`"`)
		if r.URL.Query().Get("v") == f.ver { // the page asked for exactly this content
			h.Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			h.Set("Cache-Control", "no-cache") // an old or bare URL: revalidate with the ETag
		}
		if r.Header.Get("If-None-Match") == h.Get("ETag") {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		body := f.body
		if f.gz != nil && strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			h.Set("Content-Encoding", "gzip")
			body = f.gz
		}
		h.Set("Content-Length", itoa(len(body)))
		if r.Method != http.MethodHead {
			_, _ = w.Write(body)
		}
	})
}
