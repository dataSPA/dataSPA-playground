package gist

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// ClonePlayground clones a gist's git repo to destDir, then expands the
// flat __ -encoded filenames back into a proper directory structure.
func (c *Client) ClonePlayground(ctx context.Context, gistID string, destDir string) error {
	// Fetch gist to get the clone URL
	g, _, err := c.gh.Gists.Get(ctx, gistID)
	if err != nil {
		return fmt.Errorf("fetching gist %s: %w", gistID, err)
	}

	cloneURL := g.GetGitPullURL()
	if cloneURL == "" {
		return fmt.Errorf("gist %s has no git pull URL", gistID)
	}

	// Embed token for authenticated access to secret gists
	if c.token != "" {
		cloneURL = injectTokenInURL(cloneURL, c.token)
	}

	// Clone into a temp dir first, then reorganize into destDir
	tmpClone, err := os.MkdirTemp("", "ds-pen-clone-*")
	if err != nil {
		return fmt.Errorf("creating temp clone dir: %w", err)
	}
	defer os.RemoveAll(tmpClone)

	cmd := exec.CommandContext(ctx, "git", "clone", cloneURL, tmpClone)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git clone: %w", err)
	}

	// Walk the cloned files, decode paths, write to destDir
	entries, err := os.ReadDir(tmpClone)
	if err != nil {
		return fmt.Errorf("reading cloned dir: %w", err)
	}

	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("creating dest dir: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue // skip .git etc.
		}

		name := entry.Name()
		relPath := DecodePath(name)

		srcPath := filepath.Join(tmpClone, name)
		dstPath := filepath.Join(destDir, filepath.FromSlash(relPath))

		content, err := os.ReadFile(srcPath)
		if err != nil {
			return fmt.Errorf("reading %s: %w", name, err)
		}

		if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
			return fmt.Errorf("creating dir for %s: %w", relPath, err)
		}

		if err := os.WriteFile(dstPath, content, 0o644); err != nil {
			return fmt.Errorf("writing %s: %w", relPath, err)
		}
	}

	return nil
}

// injectTokenInURL rewrites an HTTPS URL to include an oauth2 token for
// authenticated access: https://gist.github.com/ID.git →
// https://oauth2:TOKEN@gist.github.com/ID.git
func injectTokenInURL(rawURL, token string) string {
	const prefix = "https://"
	if len(rawURL) > len(prefix) && rawURL[:len(prefix)] == prefix {
		return prefix + "oauth2:" + token + "@" + rawURL[len(prefix):]
	}
	return rawURL
}
