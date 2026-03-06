package session

import (
	"io"
	"regexp"
	"sync/atomic"
)

var claudeURLPattern = regexp.MustCompile(`https://claude\.ai/[^\s"'\x60\x00-\x1f]+`)

// urlScanner wraps an io.Writer and scans written data for a Claude URL.
// Fires the callback exactly once when the first URL is detected.
type urlScanner struct {
	inner    io.Writer
	callback func(url string)
	found    atomic.Bool
}

func newURLScanner(inner io.Writer, callback func(url string)) *urlScanner {
	return &urlScanner{inner: inner, callback: callback}
}

func (u *urlScanner) Write(p []byte) (n int, err error) {
	n, err = u.inner.Write(p)

	if !u.found.Load() {
		if match := claudeURLPattern.Find(p); match != nil {
			if u.found.CompareAndSwap(false, true) {
				u.callback(string(match))
			}
		}
	}

	return
}
