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

	sprig "github.com/go-task/slim-sprig/v3"
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

	section := allSections[0]

	loop := section.frontmatter.Loop
	interval := section.frontmatter.Interval
	count := section.frontmatter.Count

	// For looping mode, use session position tracking
	pos := 0
	if loop && interval > 0 {
		pos = h.sessions.GetSeqPos(sd, urlPath+":sse:"+r.Method)
		if pos >= len(allSections) {
			pos = len(allSections) - 1
		}
		section = allSections[pos]
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

		// Count mode: track progress through the current file group
		var groupStart, groupLen, groupTicks int
		if count > 0 {
			groupStart = fileGroupStart(allSections, pos)
			groupLen = fileGroupLen(allSections, groupStart)
			groupTicks = 1 // initial send counts
		}

		for {
			select {
			case <-r.Context().Done():
				return
			case <-ticker.C:
				if count > 0 {
					// Check if current file group's loops are exhausted
					if groupTicks >= count*groupLen {
						nextStart := groupStart + groupLen
						if nextStart >= len(allSections) {
							return // No more files, close connection
						}

						nextSection := allSections[nextStart]
						groupStart = nextStart
						groupLen = fileGroupLen(allSections, nextStart)
						count = nextSection.frontmatter.Count
						groupTicks = 0

						if nextSection.frontmatter.Interval > 0 {
							ticker.Reset(time.Duration(nextSection.frontmatter.Interval) * time.Millisecond)
						}

						// If next file doesn't loop, send all its sections once and close
						if !nextSection.frontmatter.Loop || nextSection.frontmatter.Interval <= 0 {
							for i := 0; i < groupLen; i++ {
								loopIteration++
								messageCount++
								td.GlobalHits = h.counters.GetGlobalHits()
								td.URLHits = h.counters.GetURLHits(urlPath)
								td.SSEMessageCount = messageCount
								td.LoopIteration = loopIteration
								if err := h.sendSSESection(sse, allSections, nextStart+i, td); err != nil {
									return
								}
							}
							return
						}
					}
					loopPos = groupStart + (groupTicks % groupLen)
					groupTicks++
				} else {
					loopPos++
					loopPos = loopPos % len(allSections)
				}
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
		// Sequential mode: send all sections from the beginning with a delay between each.
		delay := section.frontmatter.Delay
		if delay <= 0 {
			delay = 5000 // default 5 seconds
		}

		messageCount := int64(1)
		td.SSEMessageCount = messageCount
		td.LoopIteration = 0

		for i := 1; i < len(allSections); i++ {
			select {
			case <-r.Context().Done():
				return
			case <-time.After(time.Duration(delay) * time.Millisecond):
				messageCount++
				td.GlobalHits = h.counters.GetGlobalHits()
				td.URLHits = h.counters.GetURLHits(urlPath)
				td.SSEMessageCount = messageCount

				if err := h.sendSSESection(sse, allSections, i, td); err != nil {
					return
				}
			}
		}

		// All sections sent — keep connection open for NATS messages
		for {
			select {
			case <-r.Context().Done():
				return
			case msg := <-natsCh:
				h.mergeNATSSignals(msg.Data, &td)
				td.GlobalHits = h.counters.GetGlobalHits()
				td.URLHits = h.counters.GetURLHits(urlPath)
				messageCount++
				td.SSEMessageCount = messageCount

				if err := h.sendSSESection(sse, allSections, len(allSections)-1, td); err != nil {
					return
				}
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
	fileIndex   int // index of the source file in the files slice
}

// collectSections flattens files and their sections into a linear sequence.
func collectSections(files []*ParsedFile) []sectionEntry {
	var entries []sectionEntry
	for i, f := range files {
		for _, s := range f.Sections {
			entries = append(entries, sectionEntry{
				content:     s,
				frontmatter: f.Frontmatter,
				fileIndex:   i,
			})
		}
	}
	return entries
}

// fileGroupStart returns the index of the first section belonging to the same file as sections[pos].
func fileGroupStart(sections []sectionEntry, pos int) int {
	fi := sections[pos].fileIndex
	start := pos
	for start > 0 && sections[start-1].fileIndex == fi {
		start--
	}
	return start
}

// fileGroupLen returns the number of consecutive sections starting at start that belong to the same file.
func fileGroupLen(sections []sectionEntry, start int) int {
	if start >= len(sections) {
		return 0
	}
	fi := sections[start].fileIndex
	length := 1
	for start+length < len(sections) && sections[start+length].fileIndex == fi {
		length++
	}
	return length
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
	tmpl, err := template.New("page").Funcs(sprig.FuncMap()).Parse(content)
	if err != nil {
		return "", fmt.Errorf("parsing template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, td); err != nil {
		return "", fmt.Errorf("executing template: %w", err)
	}

	return buf.String(), nil
}
