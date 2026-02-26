package server

import (
	"fmt"
	"log"
	"net/http"
	"path/filepath"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

type Config struct {
	Port           int
	PlaygroundsDir string
	SessionSecret  string
	Debug          bool
}

func Run(cfg Config) error {
	// Start embedded NATS
	ns, nc, err := StartEmbeddedNATS()
	if err != nil {
		return fmt.Errorf("starting nats: %w", err)
	}
	defer ns.Shutdown()
	defer nc.Close()

	counters := NewCounters()
	sessions := NewSessionManager(cfg.SessionSecret)
	handler := NewHandler(cfg.PlaygroundsDir, counters, sessions, nc, cfg.Debug)

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	// Static file serving
	fs := http.FileServer(http.Dir(filepath.Join(cfg.PlaygroundsDir, "static")))
	r.Handle("/static/*", http.StripPrefix("/static", fs))

	// Catch-all: every request goes through the playground handler
	r.HandleFunc("/*", handler.ServePlayground)

	if cfg.Debug {
		routes, err := ScanPlaygrounds(cfg.PlaygroundsDir)
		if err != nil {
			log.Printf("[debug] error scanning route table: %v", err)
		} else {
			log.Printf("[debug] route table (%d routes):", len(routes))
			for urlPath, rf := range routes {
				for method, files := range rf.HTMLFiles {
					m := method
					if m == "" {
						m = "*"
					}
					for _, f := range files {
						log.Printf("[debug]   %s %s → HTML %s (sections=%d, seq=%d)", m, urlPath, f.Path, len(f.Sections), f.SeqIndex)
					}
				}
				for method, files := range rf.SSEFiles {
					m := method
					if m == "" {
						m = "*"
					}
					for _, f := range files {
						log.Printf("[debug]   %s %s → SSE  %s (sections=%d, seq=%d)", m, urlPath, f.Path, len(f.Sections), f.SeqIndex)
					}
				}
			}
		}
	}

	addr := fmt.Sprintf(":%d", cfg.Port)
	log.Printf("ds-play listening on http://localhost%s", addr)
	log.Printf("Serving playgrounds from: %s", cfg.PlaygroundsDir)
	return http.ListenAndServe(addr, r)
}
