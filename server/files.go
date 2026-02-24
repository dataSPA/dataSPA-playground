package server

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Frontmatter holds the parsed header of a template file.
type Frontmatter struct {
	Loop     bool `yaml:"loop"`
	Interval int  `yaml:"interval"` // milliseconds between loop iterations
	Status   int  `yaml:"status"`   // HTTP status code (0 means use default: 200)
}

// ParsedFile represents a single template file parsed into frontmatter + response sections.
type ParsedFile struct {
	Frontmatter Frontmatter
	Sections    []string // template bodies (split by ===), may include empty strings
	Path        string   // original file path on disk
	SeqIndex    int      // sequence index from _NNN suffix (-1 if none)
}

// RouteFiles holds all the files for a given route, keyed by HTTP method.
// Empty string key "" means "any method" (fallback).
type RouteFiles struct {
	HTMLFiles map[string][]*ParsedFile // method → files for regular HTML responses
	SSEFiles  map[string][]*ParsedFile // method → files for SSE responses
}

func (rf *RouteFiles) LookupHTML(method string) []*ParsedFile {
	if files, ok := rf.HTMLFiles[strings.ToUpper(method)]; ok && len(files) > 0 {
		return files
	}
	return rf.HTMLFiles[""]
}

func (rf *RouteFiles) LookupSSE(method string) []*ParsedFile {
	if files, ok := rf.SSEFiles[strings.ToUpper(method)]; ok && len(files) > 0 {
		return files
	}
	return rf.SSEFiles[""]
}

const frontmatterSeparator = "---"
const sectionSeparator = "==="

// ParseFile reads and parses a template file from disk.
func ParseFile(path string) (*ParsedFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	content := string(data)
	pf := &ParsedFile{
		Path:     path,
		SeqIndex: -1,
	}

	// Parse frontmatter
	if strings.HasPrefix(strings.TrimSpace(content), frontmatterSeparator) {
		trimmed := strings.TrimSpace(content)
		rest := trimmed[len(frontmatterSeparator):]
		endIdx := strings.Index(rest, "\n"+frontmatterSeparator)
		if endIdx >= 0 {
			fmContent := rest[:endIdx]
			if err := yaml.Unmarshal([]byte(fmContent), &pf.Frontmatter); err != nil {
				return nil, err
			}
			afterClose := rest[endIdx+len("\n"+frontmatterSeparator):]
			content = strings.TrimPrefix(afterClose, "\n")
		}
	}

	// Split body into sections — keep empty sections (they represent empty responses)
	sections := strings.Split(content, "\n"+sectionSeparator+"\n")
	for _, s := range sections {
		pf.Sections = append(pf.Sections, strings.TrimSpace(s))
	}

	if len(pf.Sections) == 0 {
		pf.Sections = []string{""}
	}

	return pf, nil
}

// extractSeqIndex extracts the _NNN sequence index from a filename stem.
// Returns the base name (without _NNN) and the index (-1 if none).
func extractSeqIndex(stem string) (string, int) {
	lastUnderscore := strings.LastIndex(stem, "_")
	if lastUnderscore < 0 {
		return stem, -1
	}

	suffix := stem[lastUnderscore+1:]
	if n, err := strconv.Atoi(suffix); err == nil && len(suffix) > 0 {
		return stem[:lastUnderscore], n
	}
	return stem, -1
}

var knownMethods = map[string]bool{
	"get": true, "post": true, "put": true, "patch": true, "delete": true,
}

// classifyFile determines the file type from its stem (filename without .html).
//
// Well-known filenames within a directory:
//
//	index.html        → HTML, any method
//	sse.html          → SSE, any method
//	get.html          → HTML, GET
//	post.html         → HTML, POST
//	post_sse.html     → SSE, POST
//	sse_001.html      → SSE, any method, sequence 1
//	post_sse_001.html → SSE, POST, sequence 1
//	post_001.html     → HTML, POST, sequence 1
//	index_001.html    → HTML, any method, sequence 1
func classifyFile(stem string) (method string, isSSE bool, seqIdx int) {
	remaining := stem

	// 1. Extract _NNN sequence suffix
	remaining, seqIdx = extractSeqIndex(remaining)

	// 2. Check for _sse suffix (or exactly "sse")
	if strings.ToLower(remaining) == "sse" {
		return "", true, seqIdx
	}
	if strings.HasSuffix(strings.ToLower(remaining), "_sse") {
		isSSE = true
		remaining = remaining[:len(remaining)-4]
	}

	// 3. Check if remaining is a known method
	if knownMethods[strings.ToLower(remaining)] {
		return strings.ToUpper(remaining), isSSE, seqIdx
	}

	// 4. "index" or anything else → HTML, any method
	return "", isSSE, seqIdx
}

// ScanPlaygrounds scans the playgrounds directory and returns a map of URL path → RouteFiles.
// The directory path becomes the URL. Files within each directory are handlers:
//
//	index.html    → default HTML handler
//	sse.html      → SSE handler
//	post.html     → POST-specific HTML handler
//	post_sse.html → POST-specific SSE handler
func ScanPlaygrounds(root string) (map[string]*RouteFiles, error) {
	routes := make(map[string]*RouteFiles)

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if filepath.Ext(path) != ".html" {
			return nil
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}

		// The directory path is the URL
		dir := filepath.Dir(rel)
		var urlPath string
		if dir == "." {
			urlPath = "/"
		} else {
			urlPath = "/" + filepath.ToSlash(dir) + "/"
		}

		stem := strings.TrimSuffix(filepath.Base(rel), ".html")
		method, isSSE, seqIdx := classifyFile(stem)

		pf, parseErr := ParseFile(path)
		if parseErr != nil {
			return parseErr
		}
		pf.SeqIndex = seqIdx

		if _, ok := routes[urlPath]; !ok {
			routes[urlPath] = &RouteFiles{
				HTMLFiles: make(map[string][]*ParsedFile),
				SSEFiles:  make(map[string][]*ParsedFile),
			}
		}

		if isSSE {
			routes[urlPath].SSEFiles[method] = append(routes[urlPath].SSEFiles[method], pf)
		} else {
			routes[urlPath].HTMLFiles[method] = append(routes[urlPath].HTMLFiles[method], pf)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	// Sort sequential files by SeqIndex
	for _, rf := range routes {
		for _, files := range rf.HTMLFiles {
			sort.Slice(files, func(i, j int) bool {
				return files[i].SeqIndex < files[j].SeqIndex
			})
		}
		for _, files := range rf.SSEFiles {
			sort.Slice(files, func(i, j int) bool {
				return files[i].SeqIndex < files[j].SeqIndex
			})
		}
	}

	return routes, nil
}
