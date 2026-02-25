package gist

import (
	"path/filepath"
	"strings"
)

const pathSeparator = "__"

// EncodePath converts a relative file path (e.g. "home/greeting/sse.html")
// into a flat gist filename (e.g. "home__greeting__sse.html").
func EncodePath(relPath string) string {
	// Normalize to forward slashes
	relPath = filepath.ToSlash(relPath)
	return strings.ReplaceAll(relPath, "/", pathSeparator)
}

// DecodePath converts a flat gist filename (e.g. "home__greeting__sse.html")
// back into a relative file path (e.g. "home/greeting/sse.html").
func DecodePath(gistFilename string) string {
	return strings.ReplaceAll(gistFilename, pathSeparator, "/")
}
