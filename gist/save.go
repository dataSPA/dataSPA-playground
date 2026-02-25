package gist

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/google/go-github/v68/github"
)

// SaveOptions controls how a playground is saved to a gist.
type SaveOptions struct {
	Public      bool
	Description string
}

// SavePlayground walks a playground directory, encodes all .html files into
// flat gist filenames, and creates a new GitHub gist. Returns the gist ID and
// HTML URL.
func (c *Client) SavePlayground(ctx context.Context, dir string, opts SaveOptions) (gistID string, htmlURL string, err error) {
	files := make(map[github.GistFilename]github.GistFile)

	err = filepath.Walk(dir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		if filepath.Ext(path) != ".html" {
			return nil
		}

		rel, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			return relErr
		}

		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}

		gistName := EncodePath(filepath.ToSlash(rel))
		files[github.GistFilename(gistName)] = github.GistFile{
			Content: github.Ptr(string(content)),
		}
		return nil
	})
	if err != nil {
		return "", "", fmt.Errorf("walking playground dir: %w", err)
	}

	if len(files) == 0 {
		return "", "", fmt.Errorf("no .html files found in %s", dir)
	}

	desc := opts.Description
	if desc == "" {
		desc = "ds-pen playground"
	}

	g := &github.Gist{
		Description: github.Ptr(desc),
		Public:      github.Ptr(opts.Public),
		Files:       files,
	}

	created, _, apiErr := c.gh.Gists.Create(ctx, g)
	if apiErr != nil {
		return "", "", fmt.Errorf("creating gist: %w", apiErr)
	}

	return created.GetID(), created.GetHTMLURL(), nil
}
