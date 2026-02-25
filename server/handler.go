package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/sessions"
	"github.com/nats-io/nats.go"
	"github.com/starfederation/datastar-go/datastar"
)

// TemplateData is the data passed to every template.
type TemplateData struct {
	GlobalHits      int64
	URLHits         int64
	SessionURLHits  int64
	Username        string
	SessionID       string
	URL             string
	Method          string
	Signals         map[string]any
	SSEMessageCount int64
	LoopIteration   int64
}

// Handler handles playground requests.
type Handler struct {
	playgroundsDir string
	counters       *Counters
	sessions       *SessionManager
	nc             *nats.Conn
}

func NewHandler(playgroundsDir string, counters *Counters, sessions *SessionManager, nc *nats.Conn) *Handler {
	return &Handler{
		playgroundsDir: playgroundsDir,
		counters:       counters,
		sessions:       sessions,
		nc:             nc,
	}
}

func (h *Handler) TestFunc(w http.ResponseWriter, r *http.Request) {
	log.Printf("datastar req: %v", r.Header.Get("datastar-request") != "")
	if r.Header.Get("datastar-request") == "" {
		w.Write([]byte(`
			<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>ds-play — Datastar Playground</title>
    <script type="module" src="https://cdn.jsdelivr.net/gh/starfederation/datastar@v1.0.0-RC.7/bundles/datastar.js"></script>
    <style>
        body { font-family: system-ui, sans-serif; max-width: 800px; margin: 2rem auto; padding: 0 1rem; }
        h1 { color: #333; }
        .info { background: #f0f0f0; padding: 1rem; border-radius: 8px; margin: 1rem 0; }
        .info dt { font-weight: bold; }
        .info dd { margin: 0 0 0.5rem 0; }
        a { color: #0066cc; }
        #sse-output { border: 2px solid #ddd; padding: 1rem; border-radius: 8px; margin: 1rem 0; min-height: 3rem; }
    </style>
</head>
<body>
    <h1>🚀 ds-play — Datastar Playground</h1>
    <p id="time">Initial time</p>
    <div data-init="@get('/test/')"></div>
</body>
</html>
		`))
	} else {
		sse := datastar.NewSSE(w, r)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				now := time.Now().Format(time.RFC3339)
				// h.sendSSESection(sse, fmt.Sprintf(`<p id="time">{.TIME}</p>`, now))
				sse.PatchElements(fmt.Sprintf(`<p id="time">%s</p>`, now))
			case <-r.Context().Done():
				return
			}
		}
	}
}

// ServePlayground handles all playground requests.
func (h *Handler) ServePlayground(w http.ResponseWriter, r *http.Request) {
	urlPath := r.URL.Path

	if urlPath == "" {
		urlPath = "/"
	}
	if urlPath != "/" && !strings.HasSuffix(urlPath, "/") {
		urlPath += "/"
	}

	// Scan files fresh each request (hot reload)
	routes, err := ScanPlaygrounds(h.playgroundsDir)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error scanning playgrounds: %v", err), http.StatusInternalServerError)
		return
	}

	rf, ok := routes[urlPath]
	if !ok {
		http.NotFound(w, r)
		return
	}

	isDatastarRequest := r.Header.Get("datastar-request") != ""

	// Read signals from the request (must happen before NewSSE for POST bodies)
	signals := map[string]any{}
	if isDatastarRequest {
		if err := datastar.ReadSignals(r, &signals); err != nil {
			log.Printf("Warning: failed to read signals: %v", err)
		}
	}

	// Get/create session
	sess, sd, err := h.sessions.GetOrCreate(w, r)
	if err != nil {
		http.Error(w, fmt.Sprintf("Session error: %v", err), http.StatusInternalServerError)
		return
	}

	// Bump counters
	globalHits, urlHits := h.counters.Hit(urlPath)
	sessionURLHits := h.sessions.IncrementURLHits(w, r, sess, sd, urlPath)

	td := TemplateData{
		GlobalHits:     globalHits,
		URLHits:        urlHits,
		SessionURLHits: sessionURLHits,
		Username:       sd.Username,
		SessionID:      sd.SessionID,
		URL:            urlPath,
		Method:         r.Method,
		Signals:        signals,
	}

	// Route to SSE or HTML handler based on datastar-request header
	if isDatastarRequest {
		sseFiles := rf.LookupSSE(r.Method)
		if len(sseFiles) > 0 {
			h.handleSSE(w, r, sseFiles, sess, sd, td, urlPath)
			return
		}
		// No SSE files for this method — fall through to HTML
		// (Datastar can also handle text/html responses)
	}

	htmlFiles := rf.LookupHTML(r.Method)
	if len(htmlFiles) > 0 {
		h.handleHTML(w, r, htmlFiles, isDatastarRequest, sess, sd, td, urlPath)
		return
	}

	http.NotFound(w, r)
}

func (h *Handler) handleHTML(w http.ResponseWriter, r *http.Request, files []*ParsedFile, isDatastarRequest bool, sess *sessions.Session, sd *SessionData, td TemplateData, urlPath string) {
	allSections := collectSections(files)

	pos := h.sessions.GetSeqPos(sd, urlPath+":html:"+r.Method)
	if pos >= len(allSections) {
		pos = len(allSections) - 1
	}

	section := allSections[pos]

	// Advance sequence for next request (before writing response so cookie is set)
	if len(allSections) > 1 {
		h.sessions.AdvanceSeqPos(w, r, sess, sd, urlPath+":html:"+r.Method, len(allSections), section.frontmatter.Loop)
	}

	// Publish signals to NATS for listening SSE connections
	if isDatastarRequest && len(td.Signals) > 0 {
		h.publishSignals(td)
	}

	status := section.frontmatter.Status

	// Empty response
	if section.content == "" {
		if status == 0 {
			status = http.StatusNoContent
		}
		w.WriteHeader(status)
		return
	}

	rendered, err := renderTemplate(section.content, td)
	if err != nil {
		http.Error(w, fmt.Sprintf("Template error: %v", err), http.StatusInternalServerError)
		return
	}

	if status == 0 {
		status = http.StatusOK
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	w.Write([]byte(rendered))
}

func (h *Handler) handleSSE(w http.ResponseWriter, r *http.Request, files []*ParsedFile, sess *sessions.Session, sd *SessionData, td TemplateData, urlPath string) {
	allSections := collectSections(files)

	pos := h.sessions.GetSeqPos(sd, urlPath+":sse:"+r.Method)
	if pos >= len(allSections) {
		pos = len(allSections) - 1
	}

	section := allSections[pos]

	loop := section.frontmatter.Loop
	interval := section.frontmatter.Interval

	// Advance sequence position before SSE headers flush (for non-looping multi-step)
	if !(loop && interval > 0) && len(allSections) > 1 {
		h.sessions.AdvanceSeqPos(w, r, sess, sd, urlPath+":sse:"+r.Method, len(allSections), loop)
	}

	// Create SSE writer (flushes headers — no more cookie changes after this)
	sse := datastar.NewSSE(w, r)

	// Set up NATS subscriptions
	natsCh := make(chan *nats.Msg, 16)
	var subs []*nats.Subscription

	sessionSubject := fmt.Sprintf("dspen.session.%s", sd.SessionID)
	if sub, err := h.nc.ChanSubscribe(sessionSubject, natsCh); err == nil {
		subs = append(subs, sub)
	} else {
		log.Printf("NATS subscribe error (session): %v", err)
	}

	if tabID, ok := td.Signals["tab_id"].(string); ok && tabID != "" {
		tabSubject := fmt.Sprintf("dspen.tab.%s", tabID)
		if sub, err := h.nc.ChanSubscribe(tabSubject, natsCh); err == nil {
			subs = append(subs, sub)
		} else {
			log.Printf("NATS subscribe error (tab): %v", err)
		}
	}

	defer func() {
		for _, sub := range subs {
			sub.Unsubscribe()
		}
	}()

	// Send the initial response (skip if empty)
	if section.content != "" {
		if err := h.sendSSESection(sse, allSections, pos, td); err != nil {
			log.Printf("Error sending initial response: %v", err)
			return
		}
	}

	if loop && interval > 0 {
		// Looping mode: ticker + NATS
		ticker := time.NewTicker(time.Duration(interval) * time.Millisecond)
		defer ticker.Stop()

		loopPos := pos
		loopIteration := int64(0)
		messageCount := int64(1) // Count initial message
		for {
			select {
			case <-r.Context().Done():
				return
			case <-ticker.C:
				loopPos++
				loopPos = loopPos % len(allSections)
				loopIteration++

				td.GlobalHits = h.counters.GetGlobalHits()
				td.URLHits = h.counters.GetURLHits(urlPath)
				td.SSEMessageCount = messageCount
				td.LoopIteration = loopIteration

				if err := h.sendSSESection(sse, allSections, loopPos, td); err != nil {
					return
				}
				messageCount++
			case msg := <-natsCh:
				h.mergeNATSSignals(msg.Data, &td)
				td.GlobalHits = h.counters.GetGlobalHits()
				td.URLHits = h.counters.GetURLHits(urlPath)
				td.SSEMessageCount = messageCount
				td.LoopIteration = loopIteration

				if err := h.sendSSESection(sse, allSections, loopPos, td); err != nil {
					return
				}
				messageCount++
			}
		}
	} else {
		// Stay open listening for NATS messages
		messageCount := int64(1) // Count initial message
		td.SSEMessageCount = messageCount
		td.LoopIteration = 0
		for {
			select {
			case <-r.Context().Done():
				return
				// case msg := <-natsCh:
				// 	h.mergeNATSSignals(msg.Data, &td)
				// 	td.GlobalHits = h.counters.GetGlobalHits()
				// 	td.URLHits = h.counters.GetURLHits(urlPath)

				// 	if err := h.sendSSESection(sse, allSections, pos, td); err != nil {
				// 		return
				// 	}
			}
		}
	}
}

// publishSignals publishes the current signals to NATS on tab and session subjects.
func (h *Handler) publishSignals(td TemplateData) {
	data, err := json.Marshal(td.Signals)
	if err != nil {
		log.Printf("Failed to marshal signals for NATS publish: %v", err)
		return
	}

	// Publish to session subject
	subject := fmt.Sprintf("dspen.session.%s", td.SessionID)
	if err := h.nc.Publish(subject, data); err != nil {
		log.Printf("NATS publish error (session): %v", err)
	}

	// Publish to tab subject if present
	if tabID, ok := td.Signals["tab_id"].(string); ok && tabID != "" {
		subject := fmt.Sprintf("dspen.tab.%s", tabID)
		if err := h.nc.Publish(subject, data); err != nil {
			log.Printf("NATS publish error (tab): %v", err)
		}
	}
}

// mergeNATSSignals merges JSON signal data from a NATS message into the template data.
func (h *Handler) mergeNATSSignals(data []byte, td *TemplateData) {
	if len(data) == 0 {
		return
	}
	var incoming map[string]any
	if err := json.Unmarshal(data, &incoming); err != nil {
		log.Printf("NATS message unmarshal error: %v", err)
		return
	}
	for k, v := range incoming {
		td.Signals[k] = v
	}
}

// sectionEntry pairs a template body with its parent file's frontmatter.
type sectionEntry struct {
	content     string
	frontmatter Frontmatter
}

// collectSections flattens files and their sections into a linear sequence.
func collectSections(files []*ParsedFile) []sectionEntry {
	var entries []sectionEntry
	for _, f := range files {
		for _, s := range f.Sections {
			entries = append(entries, sectionEntry{
				content:     s,
				frontmatter: f.Frontmatter,
			})
		}
	}
	return entries
}

func (h *Handler) sendSSESection(sse *datastar.ServerSentEventGenerator, sections []sectionEntry, pos int, td TemplateData) error {
	if pos >= len(sections) {
		pos = len(sections) - 1
	}

	section := sections[pos]

	// Empty section — skip PatchElements but don't error
	if section.content == "" {
		return nil
	}

	rendered, err := renderTemplate(section.content, td)
	if err != nil {
		log.Printf("Template render error: %v", err)
		return err
	}

	return sse.PatchElements(rendered)
}

func renderTemplate(content string, td TemplateData) (string, error) {
	tmpl, err := template.New("page").Parse(content)
	if err != nil {
		return "", fmt.Errorf("parsing template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, td); err != nil {
		return "", fmt.Errorf("executing template: %w", err)
	}

	return buf.String(), nil
}
