package gist

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ParseGistID extracts a gist ID from either a raw ID string or a full
// gist URL (e.g. "https://gist.github.com/user/abc123" → "abc123").
func ParseGistID(input string) string {
	input = strings.TrimSpace(input)
	input = strings.TrimSuffix(input, "/")
	// If it looks like a URL, take the last path segment
	if strings.Contains(input, "/") {
		parts := strings.Split(input, "/")
		return parts[len(parts)-1]
	}
	return input
}

// LoadPlayground fetches a gist by ID and returns a map of relative file
// paths to their content (decoded from the flat gist filenames).
func (c *Client) LoadPlayground(ctx context.Context, gistID string) (map[string]string, error) {
	g, _, err := c.gh.Gists.Get(ctx, gistID)
	if err != nil {
		return nil, fmt.Errorf("fetching gist %s: %w", gistID, err)
	}

	files := make(map[string]string, len(g.Files))
	for name, file := range g.Files {
		relPath := DecodePath(string(name))
		files[relPath] = file.GetContent()
	}

	return files, nil
}

// LoadToTempDir fetches a gist and writes its files into a temporary directory,
// recreating the directory structure. Returns the temp dir path.
func (c *Client) LoadToTempDir(ctx context.Context, gistID string) (string, error) {
	files, err := c.LoadPlayground(ctx, gistID)
	if err != nil {
		return "", err
	}

	tmpDir, err := os.MkdirTemp("", "ds-play-gist-*")
	if err != nil {
		return "", fmt.Errorf("creating temp dir: %w", err)
	}

	for relPath, content := range files {
		fullPath := filepath.Join(tmpDir, filepath.FromSlash(relPath))
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			os.RemoveAll(tmpDir)
			return "", fmt.Errorf("creating dir for %s: %w", relPath, err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
			os.RemoveAll(tmpDir)
			return "", fmt.Errorf("writing %s: %w", relPath, err)
		}
	}

	return tmpDir, nil
}
