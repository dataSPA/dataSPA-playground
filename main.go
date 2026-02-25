package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/felixingram/ds-pen/gist"
	"github.com/felixingram/ds-pen/server"
)

func main() {
	port := flag.Int("port", 8080, "port to listen on")
	dir := flag.String("dir", "", "playgrounds directory (default: ./playgrounds)")
	secret := flag.String("secret", "ds-pen-dev-secret-change-me", "session cookie secret")

	// Gist flags
	gistFlag := flag.String("gist", "", "gist ID or URL to load as playground source")
	githubToken := flag.String("github-token", "", "GitHub personal access token (or set GITHUB_TOKEN)")
	clone := flag.Bool("clone", false, "clone gist to disk instead of serving from memory")
	cloneDir := flag.String("clone-dir", "", "directory to clone gist into (default: ./playgrounds)")
	saveGist := flag.Bool("save-gist", false, "save the current playground directory to a new gist and exit")
	gistPublic := flag.Bool("gist-public", false, "make saved gist public (default: secret)")
	gistDescription := flag.String("gist-description", "", "description for saved gist")

	flag.Parse()

	// Resolve GitHub token: flag takes precedence over env var
	token := *githubToken
	if token == "" {
		token = os.Getenv("GITHUB_TOKEN")
	}

	// Handle --save-gist: save playground dir to a new gist and exit
	if *saveGist {
		saveDir := resolvePlaygroundsDir(*dir)
		if token == "" {
			log.Fatal("--save-gist requires a GitHub token (--github-token or GITHUB_TOKEN)")
		}
		gc := gist.NewClient(token)
		id, htmlURL, err := gc.SavePlayground(context.Background(), saveDir, gist.SaveOptions{
			Public:      *gistPublic,
			Description: *gistDescription,
		})
		if err != nil {
			log.Fatalf("Failed to save gist: %v", err)
		}
		fmt.Printf("Gist created: %s\n", htmlURL)
		fmt.Printf("Load with:    ds-pen --gist %s\n", id)
		return
	}

	// Handle --gist: load playground from a gist
	playgroundsDir := resolvePlaygroundsDir(*dir)
	var tempDir string

	if *gistFlag != "" {
		gistID := gist.ParseGistID(*gistFlag)
		gc := gist.NewClient(token)
		ctx := context.Background()

		if *clone {
			// Clone to disk
			dest := *cloneDir
			if dest == "" {
				dest = playgroundsDir
			}
			log.Printf("Cloning gist %s to %s...", gistID, dest)
			if err := gc.ClonePlayground(ctx, gistID, dest); err != nil {
				log.Fatalf("Failed to clone gist: %v", err)
			}
			playgroundsDir = dest
		} else {
			// Load into memory (temp dir)
			log.Printf("Loading gist %s into memory...", gistID)
			tmpDir, err := gc.LoadToTempDir(ctx, gistID)
			if err != nil {
				log.Fatalf("Failed to load gist: %v", err)
			}
			tempDir = tmpDir
			playgroundsDir = tmpDir
		}
	}

	// Clean up temp dir on exit
	if tempDir != "" {
		defer os.RemoveAll(tempDir)
	}

	// Ensure playgrounds directory exists
	if _, err := os.Stat(playgroundsDir); os.IsNotExist(err) {
		log.Fatalf("Playgrounds directory does not exist: %s", playgroundsDir)
	}

	cfg := server.Config{
		Port:           *port,
		PlaygroundsDir: playgroundsDir,
		SessionSecret:  *secret,
	}

	if err := server.Run(cfg); err != nil {
		log.Fatal(err)
	}
}

func resolvePlaygroundsDir(dir string) string {
	if dir != "" {
		return dir
	}
	wd, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	return filepath.Join(wd, "playgrounds")
}
