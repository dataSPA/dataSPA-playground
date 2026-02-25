# ds-play: Datastar Playground Engine

![dataSPA Playground](./playtime.png)

A single Go binary that serves interactive Datastar playgrounds with file-based routing, server-sent events (SSE), session management, and real-time messaging.

## Features

- **File-based routing**: Directory structure maps directly to URL paths
- **SSE support**: Create a sequence of SSE events or loop a particular template
- **Template variables**: Access to session data, hit counters, and request context

## Installation

```bash
go install github.com/dataSPA/dataSPA-playground@latest
```

Or clone and build:

```bash
git clone https://github.com/dataSPA/dataSPA-playground
cd dataSPA-playground
go build -o dsplay
```

## Quick Start

### 1. Create a new playground

```bash
dsplay init my-playground
cd my-playground
```

This creates a skeleton directory structure with example files.

### 2. Serve it locally

```bash
dsplay
```

The playground is now available at `http://localhost:8080`.

### 3. Share it

```bash
dsplay share --github-token YOUR_GITHUB_TOKEN --description "My first playground"
```

This publishes your playground to a GitHub gist. You can then serve it anywhere:

```bash
dsplay serve <gist-id>
```

## Project Structure

Each playground is a directory tree where:
- **Directories** become URL paths
- **Special filenames** define handlers

### Well-Known Filenames

| Filename | Behavior |
|----------|----------|
| `index.html` | Default HTML handler (all methods) |
| `get.html` | GET-only HTML handler |
| `post.html` | POST-only HTML handler |
| `sse.html` | SSE handler (responds to Datastar signals) |
| `get_sse.html` | GET-only SSE handler |
| `post_sse.html` | POST-only SSE handler |
| `sse_001.html` | Sequential SSE file #1 |
| `index_001.html` | Sequential HTML file #1 |

### Example Structure

```
my-playground/
├── home/
│   ├── index.html           → GET /home/
│   ├── greeting/
│   │   └── sse.html         → GET /home/greeting/ (SSE)
│   ├── action/
│   │   └── post.html        → POST /home/action/
│   └── counter/
│       ├── index.html       → GET /home/counter/
│       └── step/
│           ├── sse_001.html → Step 1 of sequence
│           └── sse_002.html → Step 2 of sequence
```

## File Format

Each template file consists of optional **frontmatter** followed by a **template body**:

```
---
status: 200
loop: false
interval: 1000
---
<div id="greeting">Hello, {{.Username}}!</div>
```

### Frontmatter Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `status` | int | 200 | HTTP status code |
| `loop` | bool | false | Repeat SSE responses (SSE only) |
| `interval` | int | 0 | Delay between loops in milliseconds (SSE only) |

### Multiple Responses in One File

Separate responses with `===`:

```
---
loop: false
---
<div id="counter">Count: 1</div>
===
<div id="counter">Count: 2</div>
===
<div id="counter">Count: 3</div>
```

Each section is a step in the sequence. With looping enabled, it cycles through all sections.

## Template Variables

All templates have access to:

| Variable | Type | Description |
|----------|------|-------------|
| `.GlobalHits` | int64 | Total hits across all URLs |
| `.URLHits` | int64 | Hits to this specific URL |
| `.SessionURLHits` | int64 | Hits to this URL from this session |
| `.Username` | string | Random username for this session |
| `.SessionID` | string | Session identifier |
| `.URL` | string | Current request path |
| `.Method` | string | HTTP method |
| `.Signals` | map | Datastar signals from the request |

### Example

```html
<div>
  <h1>Hello {{.Username}}!</h1>
  <p>You've visited this page {{.SessionURLHits}} times.</p>
  <p>Total visits: {{.GlobalHits}}</p>
</div>
```

## Response Types

### HTML Response

When a request does **not** include the `datastar-request` header, the server renders the template as HTML:

- Looks for: `{url}/index.html` or `{url}/{method}.html`
- Renders with template variables
- Returns as `text/html`
- 204 No Content if file is empty

### SSE Response

When a request includes the `datastar-request` header:

- Looks for: `{url}/sse.html` or `{url}/{method}_sse.html`
- Renders each section as a Datastar patch element
- Streams responses via Server-Sent Events
- Falls back to HTML response if no SSE file exists

## Sequential Responses

Files with numbered suffixes form a sequence that progresses per-session:

```
counter/
├── sse_001.html  → shown on first request
├── sse_002.html  → shown on second request
└── sse_003.html  → shown on third request
```

Or combine with in-file sections:

```
counter/
├── sse_001.html
│   Step 1a
│   ===
│   Step 1b
└── sse_002.html
    Step 2
```

After the last response, the sequence stops (or loops if `loop: true`).

## Looping SSE

Enable with frontmatter:

```
---
loop: true
interval: 2000
---
<div id="time">{{now}}</div>
```

The SSE connection stays open and re-sends responses every 2000ms, cycling through all sections, then looping back to the start.

## Session Management

Sessions are cookie-based with:

- **Duration**: 1 hour
- **Tracking**: Per-session counters for each URL
- **Random username**: Assigned automatically (e.g., "clever-fox-42")
- **Sequence position**: Each session tracks where it is in multi-step sequences independently

## Real-Time Communication (NATS)

Playgrounds have an in-process NATS server. Use it to:

1. **Send signals from HTML handlers** to listening SSE connections
2. **Broadcast updates** across sessions or specific browser tabs

### NATS Subjects

| Subject | Description |
|---------|-------------|
| `dspen.session.<session_id>` | All tabs in a session |
| `dspen.tab.<tab_id>` | Specific tab (if `tab_id` signal sent) |

When a non-SSE datastar request completes, it automatically publishes its signals to these subjects, triggering re-renders on listening SSE handlers.

## Usage

### Default (serve current directory)

```bash
dsplay
```

Serves the current directory on port 8080.

### Options

```bash
dsplay [--port 9000] [--secret your-secret] [--github-token TOKEN]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--port` | 8080 | Port to listen on |
| `--secret` | dev secret | Session cookie secret (change in production!) |
| `--github-token` | - | GitHub token (for gist operations) |

### Commands

#### `init [directory]`

Create a new playground skeleton.

```bash
dsplay init my-playground
dsplay init --force   # Overwrite existing files
```

#### `serve [directory or gist-id]`

Serve a playground.

```bash
dsplay serve /path/to/playground
dsplay serve https://gist.github.com/user/gist-id
dsplay serve abc123xyz  # Short gist ID
dsplay serve --clone abc123xyz --clone-dir ./my-copy  # Clone to disk
```

#### `share`

Publish current directory to GitHub gists.

```bash
dsplay share --github-token $GITHUB_TOKEN --public --description "My playground"
```

| Flag | Description |
|------|-------------|
| `--github-token` | Required. GitHub personal access token |
| `--public` | Make the gist public (default: secret) |
| `--description` | Gist description |
| `--dir` | Playground directory (default: current) |

## Architecture

### How Requests Work

1. **Incoming request** → Check for session cookie, create session if needed
2. **Path matching** → Build file list from directory structure
3. **File selection** → Pick handler based on method and `datastar-request` header
4. **Template rendering** → Compile and execute template with session data
5. **Response** → HTML or SSE depending on headers

### Hot Reloading

Templates are read from disk on every request—no caching. This keeps things simple and development-friendly, at the cost of slightly higher latency per request (negligible for most use cases).

## Tech Stack

- **Router**: [chi/v5](https://github.com/go-chi/chi) – lightweight HTTP router
- **Sessions**: [gorilla/sessions](https://github.com/gorilla/sessions) – cookie-based sessions
- **SSE**: [datastar-go](https://github.com/starfederation/datastar-go) – Datastar SDK
- **Templates**: Go stdlib `html/template`
- **Messaging**: Embedded [NATS server](https://nats.io/)
- **GitHub**: [go-github](https://github.com/google/go-github) – Gist API

## Examples

### Static HTML Page

`home/index.html`:
```html
---
status: 200
---
<h1>Welcome {{.Username}}</h1>
<p>Global hits: {{.GlobalHits}}</p>
```

### Form Handler

`home/form/post.html`:
```html
---
status: 204
---
```

### Multi-Step Sequence

`home/steps/sse_001.html`:
```
---
---
<div id="progress">Step 1 of 3</div>
```

`home/steps/sse_002.html`:
```
---
---
<div id="progress">Step 2 of 3</div>
```

`home/steps/sse_003.html`:
```
---
---
<div id="progress">Step 3 of 3 - Done!</div>
```

### Looping Live Update

`home/live/sse.html`:
```
---
loop: true
interval: 1000
---
<div id="clock">{{.URL}} - Hits: {{.URLHits}}</div>
```

## Development

### Building from Source

```bash
git clone https://github.com/dataSPA/dataSPA-playground
cd ds-play
go build -o dsplay
./dsplay init ./playgrounds/test
./dsplay serve ./playgrounds/test
```

### Testing

Edit any template file and refresh the browser—no rebuild needed.

## License

MIT

## Contributing

Contributions welcome! Open an issue or pull request.
