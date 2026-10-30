package dbengine

import (
	"compress/gzip"
	"io"
	"os"
	"os/user"
	"regexp"
	"strconv"
)

// chownTo gives path to the named user (no-op unless running as root with a known user).
func chownTo(path, owner string) error {
	if owner == "" || os.Geteuid() != 0 {
		return nil
	}
	u, err := user.Lookup(owner)
	if err != nil {
		return err
	}
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)
	return os.Chown(path, uid, gid)
}

func gzipWrite(src io.Reader, dstPath string) error {
	f, err := os.OpenFile(dstPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	zw := gzip.NewWriter(f)
	if _, err := io.Copy(zw, src); err != nil {
		zw.Close()
		f.Close()
		os.Remove(dstPath)
		return err
	}
	if err := zw.Close(); err != nil {
		f.Close()
		os.Remove(dstPath)
		return err
	}
	return f.Close()
}

func gunzipOpen(path string) (io.ReadCloser, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	zr, err := gzip.NewReader(f)
	if err != nil {
		f.Close()
		return nil, err
	}
	return struct {
		io.Reader
		io.Closer
	}{zr, closerFunc(func() error { zr.Close(); return f.Close() })}, nil
}

type closerFunc func() error

func (c closerFunc) Close() error { return c() }

var versionRe = regexp.MustCompile(`\d+(?:\.\d+)+`)

// firstVersion extracts the first dotted version number ("16.15", "10.11.14") from a --version output.
func firstVersion(s string) string { return versionRe.FindString(s) }
