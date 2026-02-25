package server

import (
	"encoding/gob"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/gorilla/sessions"
)

func init() {
	gob.Register(map[string]int64{})
	gob.Register(map[string]int{})
}

const (
	sessionName    = "ds-play"
	sessionMaxAge  = 3600 // 1 hour in seconds
	keyUsername    = "username"
	keySessionID  = "session_id"
	keyURLHits    = "url_hits"
	keySeqPos     = "seq_pos" // map[string]int — current sequence position per URL
)

// Counters tracks global and per-URL hit counts.
type Counters struct {
	mu         sync.RWMutex
	globalHits int64
	urlHits    map[string]*int64
}

func NewCounters() *Counters {
	return &Counters{
		urlHits: make(map[string]*int64),
	}
}

func (c *Counters) Hit(urlPath string) (globalHits int64, urlHits int64) {
	globalHits = atomic.AddInt64(&c.globalHits, 1)

	c.mu.RLock()
	counter, ok := c.urlHits[urlPath]
	c.mu.RUnlock()

	if !ok {
		c.mu.Lock()
		// Double-check after acquiring write lock
		counter, ok = c.urlHits[urlPath]
		if !ok {
			var n int64
			counter = &n
			c.urlHits[urlPath] = counter
		}
		c.mu.Unlock()
	}

	urlHits = atomic.AddInt64(counter, 1)
	return
}

func (c *Counters) GetGlobalHits() int64 {
	return atomic.LoadInt64(&c.globalHits)
}

func (c *Counters) GetURLHits(urlPath string) int64 {
	c.mu.RLock()
	counter, ok := c.urlHits[urlPath]
	c.mu.RUnlock()
	if !ok {
		return 0
	}
	return atomic.LoadInt64(counter)
}

// SessionManager handles session creation and data.
type SessionManager struct {
	store *sessions.CookieStore
}

func NewSessionManager(secret string) *SessionManager {
	store := sessions.NewCookieStore([]byte(secret))
	store.Options = &sessions.Options{
		Path:     "/",
		MaxAge:   sessionMaxAge,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	}
	return &SessionManager{store: store}
}

// SessionData holds the extracted session values for a request.
type SessionData struct {
	Username    string
	SessionID   string
	URLHits     map[string]int64
	SeqPos      map[string]int
}

// GetOrCreate retrieves or initializes a session, returning the session data.
func (sm *SessionManager) GetOrCreate(w http.ResponseWriter, r *http.Request) (*sessions.Session, *SessionData, error) {
	sess, err := sm.store.Get(r, sessionName)
	if err != nil {
		// Session decode error — create a fresh one
		sess, err = sm.store.New(r, sessionName)
		if err != nil {
			return nil, nil, fmt.Errorf("creating session: %w", err)
		}
	}

	sd := &SessionData{}

	// Username
	if v, ok := sess.Values[keyUsername].(string); ok && v != "" {
		sd.Username = v
	} else {
		sd.Username = RandomUsername()
		sess.Values[keyUsername] = sd.Username
	}

	// Session ID
	if v, ok := sess.Values[keySessionID].(string); ok && v != "" {
		sd.SessionID = v
	} else {
		sd.SessionID = fmt.Sprintf("s-%s", RandomUsername())
		sess.Values[keySessionID] = sd.SessionID
	}

	// URL hits map
	if v, ok := sess.Values[keyURLHits].(map[string]int64); ok {
		sd.URLHits = v
	} else {
		sd.URLHits = make(map[string]int64)
		sess.Values[keyURLHits] = sd.URLHits
	}

	// Sequence positions
	if v, ok := sess.Values[keySeqPos].(map[string]int); ok {
		sd.SeqPos = v
	} else {
		sd.SeqPos = make(map[string]int)
		sess.Values[keySeqPos] = sd.SeqPos
	}

	return sess, sd, nil
}

// IncrementURLHits bumps the per-session URL hit counter and saves.
func (sm *SessionManager) IncrementURLHits(w http.ResponseWriter, r *http.Request, sess *sessions.Session, sd *SessionData, urlPath string) int64 {
	sd.URLHits[urlPath]++
	sess.Values[keyURLHits] = sd.URLHits
	sess.Save(r, w)
	return sd.URLHits[urlPath]
}

// GetSeqPos returns the current sequence position for a URL.
func (sm *SessionManager) GetSeqPos(sd *SessionData, urlPath string) int {
	return sd.SeqPos[urlPath]
}

// AdvanceSeqPos increments the sequence position for a URL and saves.
func (sm *SessionManager) AdvanceSeqPos(w http.ResponseWriter, r *http.Request, sess *sessions.Session, sd *SessionData, urlPath string, totalSteps int, loop bool) {
	pos := sd.SeqPos[urlPath]
	pos++
	if loop {
		pos = pos % totalSteps
	} else if pos >= totalSteps {
		pos = totalSteps - 1 // stay on last
	}
	sd.SeqPos[urlPath] = pos
	sess.Values[keySeqPos] = sd.SeqPos
	sess.Save(r, w)
}
