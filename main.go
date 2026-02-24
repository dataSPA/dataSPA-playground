package main

import (
	"flag"
	"log"
	"os"
	"path/filepath"

	"github.com/felixingram/ds-pen/server"
)

func main() {
	port := flag.Int("port", 8080, "port to listen on")
	dir := flag.String("dir", "", "playgrounds directory (default: ./playgrounds)")
	secret := flag.String("secret", "ds-pen-dev-secret-change-me", "session cookie secret")
	flag.Parse()

	playgroundsDir := *dir
	if playgroundsDir == "" {
		// Default to ./playgrounds relative to working directory
		wd, err := os.Getwd()
		if err != nil {
			log.Fatal(err)
		}
		playgroundsDir = filepath.Join(wd, "playgrounds")
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
