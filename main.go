package main

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/dataSPA/ds-play/gist"
	"github.com/dataSPA/ds-play/server"
	"github.com/urfave/cli/v2"
)

//go:embed skeleton
var skeletonFS embed.FS

func main() {
	app := &cli.App{
		Name:  "ds-play",
		Usage: "Datastar Playground Engine",
		Flags: []cli.Flag{
			&cli.IntFlag{
				Name:  "port",
				Value: 8080,
				Usage: "port to listen on",
			},
			&cli.StringFlag{
				Name:  "secret",
				Value: "ds-play-dev-secret-change-me",
				Usage: "session cookie secret",
			},
			&cli.StringFlag{
				Name:    "github-token",
				Usage:   "GitHub personal access token",
				EnvVars: []string{"GITHUB_TOKEN"},
			},
		},
		Action: func(c *cli.Context) error {
			return runServe(c, "")
		},
		Commands: []*cli.Command{
			{
				Name:      "init",
				Usage:     "Create a skeleton playground in the current directory or a specified directory",
				ArgsUsage: "[directory]",
				Flags: []cli.Flag{
					&cli.BoolFlag{
						Name:  "force",
						Usage: "create files even if directory exists and is not empty",
					},
				},
				Action: func(c *cli.Context) error {
					return runInit(c)
				},
			},
			{
				Name:  "share",
				Usage: "Publish the current playground directory to a GitHub gist",
				Flags: []cli.Flag{
					&cli.BoolFlag{
						Name:  "public",
						Usage: "make the gist public (default: secret)",
					},
					&cli.StringFlag{
						Name:  "description",
						Usage: "description for the gist",
					},
					&cli.StringFlag{
						Name:  "dir",
						Usage: "playground directory to share (default: current directory)",
					},
				},
				Action: func(c *cli.Context) error {
					return runShare(c)
				},
			},
			{
				Name:      "serve",
				Usage:     "Serve a playground from a directory or GitHub gist URL",
				ArgsUsage: "[directory or gist URL]",
				Flags: []cli.Flag{
					&cli.BoolFlag{
						Name:  "clone",
						Usage: "clone gist to disk instead of serving from memory",
					},
					&cli.StringFlag{
						Name:  "clone-dir",
						Usage: "directory to clone gist into (default: current directory)",
					},
				},
				Action: func(c *cli.Context) error {
					return runServe(c, c.Args().First())
				},
			},
		},
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}

func runInit(c *cli.Context) error {
	var targetDir string
	var err error
	force := c.Bool("force")

	// Determine target directory
	if c.Args().Len() > 0 {
		targetDir = c.Args().Get(0)
		// Convert to absolute path
		targetDir, err = filepath.Abs(targetDir)
		if err != nil {
			return fmt.Errorf("resolving directory path: %w", err)
		}

		// Check if directory exists and is not empty (unless force is set)
		if info, err := os.Stat(targetDir); err == nil {
			if !info.IsDir() {
				return fmt.Errorf("%s exists but is not a directory", targetDir)
			}
			if !force {
				// Check if directory is not empty
				entries, err := os.ReadDir(targetDir)
				if err != nil {
					return fmt.Errorf("reading directory: %w", err)
				}
				if len(entries) > 0 {
					return fmt.Errorf("directory %s already exists and is not empty (use --force to override)", targetDir)
				}
			}
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("checking directory: %w", err)
		}

		// Create directory if it doesn't exist
		if err := os.MkdirAll(targetDir, 0o755); err != nil {
			return fmt.Errorf("creating directory: %w", err)
		}
	} else {
		// Use current working directory
		targetDir, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("getting working directory: %w", err)
		}

		// Check if home/index.html already exists in current directory (unless force is set)
		if !force {
			if _, err := os.Stat(filepath.Join(targetDir, "home", "index.html")); err == nil {
				return fmt.Errorf("home/index.html already exists (use --force to override)")
			}
		}
	}

	sub, err := fs.Sub(skeletonFS, "skeleton")
	if err != nil {
		return fmt.Errorf("reading embedded skeleton: %w", err)
	}

	err = fs.WalkDir(sub, ".", func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		dest := filepath.Join(targetDir, path)
		if d.IsDir() {
			return os.MkdirAll(dest, 0o755)
		}
		content, err := fs.ReadFile(sub, path)
		if err != nil {
			return err
		}
		return os.WriteFile(dest, content, 0o644)
	})
	if err != nil {
		return fmt.Errorf("writing skeleton files: %w", err)
	}

	fmt.Printf("Created skeleton playground at %s\n", targetDir)
	if c.Args().Len() > 0 {
		// If a specific directory was provided, show how to serve it
		fmt.Printf("Run 'ds-play serve %s' to serve it.\n", targetDir)
	} else {
		fmt.Println("Run 'ds-play' to serve it.")
	}
	return nil
}

func runShare(c *cli.Context) error {
	token := c.String("github-token")
	if token == "" {
		return fmt.Errorf("share requires a GitHub token (--github-token or GITHUB_TOKEN)")
	}

	dir := c.String("dir")
	if dir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return err
		}
		dir = wd
	}

	gc := gist.NewClient(token)
	id, htmlURL, err := gc.SavePlayground(context.Background(), dir, gist.SaveOptions{
		Public:      c.Bool("public"),
		Description: c.String("description"),
	})
	if err != nil {
		return fmt.Errorf("saving gist: %w", err)
	}

	fmt.Printf("Gist created: %s\n", htmlURL)
	fmt.Printf("Serve with:   ds-play serve %s\n", id)
	return nil
}

func runServe(c *cli.Context, source string) error {
	playgroundsDir, tempDir, err := resolveSource(c, source)
	if err != nil {
		return err
	}

	if tempDir != "" {
		defer os.RemoveAll(tempDir)
	}

	if _, err := os.Stat(playgroundsDir); os.IsNotExist(err) {
		return fmt.Errorf("playgrounds directory does not exist: %s", playgroundsDir)
	}

	cfg := server.Config{
		Port:           c.Int("port"),
		PlaygroundsDir: playgroundsDir,
		SessionSecret:  c.String("secret"),
	}

	return server.Run(cfg)
}

func resolveSource(c *cli.Context, source string) (playgroundsDir, tempDir string, err error) {
	if source == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", "", err
		}
		return wd, "", nil
	}

	if isGistSource(source) {
		return resolveGistSource(c, source)
	}

	abs, err := filepath.Abs(source)
	if err != nil {
		return "", "", err
	}
	return abs, "", nil
}

func isGistSource(source string) bool {
	return strings.Contains(source, "gist.github.com")
}

func resolveGistSource(c *cli.Context, source string) (playgroundsDir, tempDir string, err error) {
	token := c.String("github-token")
	gistID := gist.ParseGistID(source)
	gc := gist.NewClient(token)
	ctx := context.Background()

	if c.Bool("clone") {
		dest := c.String("clone-dir")
		if dest == "" {
			dest, err = os.Getwd()
			if err != nil {
				return "", "", err
			}
		}
		log.Printf("Cloning gist %s to %s...", gistID, dest)
		if err := gc.ClonePlayground(ctx, gistID, dest); err != nil {
			return "", "", fmt.Errorf("cloning gist: %w", err)
		}
		return dest, "", nil
	}

	log.Printf("Loading gist %s into memory...", gistID)
	tmpDir, err := gc.LoadToTempDir(ctx, gistID)
	if err != nil {
		return "", "", fmt.Errorf("loading gist: %w", err)
	}
	return tmpDir, tmpDir, nil
}


