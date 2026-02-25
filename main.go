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

	"github.com/felixingram/ds-pen/gist"
	"github.com/felixingram/ds-pen/server"
	"github.com/urfave/cli/v2"
)

//go:embed skeleton
var skeletonFS embed.FS

func main() {
	app := &cli.App{
		Name:  "ds-pen",
		Usage: "Datastar Playground Engine",
		Flags: []cli.Flag{
			&cli.IntFlag{
				Name:  "port",
				Value: 8080,
				Usage: "port to listen on",
			},
			&cli.StringFlag{
				Name:  "secret",
				Value: "ds-pen-dev-secret-change-me",
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
				Name:  "init",
				Usage: "Create a skeleton playground in the current directory",
				Action: func(c *cli.Context) error {
					return runInit()
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

func runInit() error {
	wd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting working directory: %w", err)
	}

	// Check if home/index.html already exists before writing anything
	if _, err := os.Stat(filepath.Join(wd, "home", "index.html")); err == nil {
		return fmt.Errorf("home/index.html already exists")
	}

	sub, err := fs.Sub(skeletonFS, "skeleton")
	if err != nil {
		return fmt.Errorf("reading embedded skeleton: %w", err)
	}

	err = fs.WalkDir(sub, ".", func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		dest := filepath.Join(wd, path)
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

	fmt.Printf("Created skeleton playground at %s\n", wd)
	fmt.Println("Run 'ds-pen' to serve it.")
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
	fmt.Printf("Serve with:   ds-pen serve %s\n", id)
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


