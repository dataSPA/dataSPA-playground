# ds-play: Datastar Playground Engine

![dataSPA Playground](./playtime.png)

A single Go binary for building and sharing interactive [Datastar](https://data-star.dev) playgrounds. Drop HTML templates into a directory, and `dsplay` turns them into a live server with file-based routing, server-sent events (SSE), session management, and real-time messaging — no build step required.

Use it to prototype Datastar ideas, share interactive demos, or teach hypermedia concepts with a tool that gets out of your way.

## Installation

```bash
go install github.com/dataSPA/dataSPA-playground@latest
```

Or build from source:

```bash
git clone https://github.com/dataSPA/dataSPA-playground
cd dataSPA-playground
go build -o dsplay
```

## Getting Started

### 1. Create a playground

Use `init` to scaffold a new playground with a working example:

```bash
dsplay init my-playground
```

This creates a `my-playground/` directory containing a ready-to-run demo with an HTML page, an SSE handler, and a POST action:

```
my-playground/
├── index.html        ← landing page (served at /)
├── sse.html          ← live-updating SSE handler
├── action/
│   └── post.html     ← POST handler for /action/
└── static/
    └── css/
```

### 2. Serve it

```bash
dsplay serve my-playground
```

Open `http://localhost:8080` in your browser. You'll see a page with a text input, a send button, and a live-updating section powered by SSE. Type a message, hit send, and watch it appear in the live updates — no page reload needed.

Edit any file in `my-playground/` and refresh your browser to see changes instantly. Templates are read from disk on every request, so there's no rebuild or restart cycle.

### 3. Share it

When you're happy with your playground, publish it as a GitHub Gist:

```bash
cd my-playground
dsplay share --description "My first playground"
```

This requires a GitHub personal access token with **gist** scope. You can create one at https://github.com/settings/personal-access-tokens and provide it via the `GITHUB_TOKEN` environment variable or the `--github-token` flag:

```bash
export GITHUB_TOKEN=ghp_your_token_here
dsplay share --description "My first playground"
```

The command prints the gist URL and a serve command. Anyone with `dsplay` can now run your playground directly from the gist:

```bash
dsplay serve https://gist.github.com/you/abc123xyz
```

The playground is fetched into memory and served locally — no clone needed. To save a local copy instead:

```bash
dsplay serve --clone https://gist.github.com/you/abc123xyz --clone-dir ./local-copy
```

## How It Works

### File-Based Routing

Your directory structure *is* your routing table. Directories become URL paths, and filenames determine the handler type and HTTP method:

| Filename | Behaviour |
|----------|-----------|
| `index.html` | HTML handler (all methods) |
| `get.html` | GET-only HTML handler |
| `post.html` | POST-only HTML handler |
| `sse.html` | SSE handler (all methods) |
| `get_sse.html` | GET-only SSE handler |
| `post_sse.html` | POST-only SSE handler |
| `sse_001.html`, `sse_002.html` | Numbered sequence (SSE) |
| `index_001.html`, `index_002.html` | Numbered sequence (HTML) |

When a request includes the `datastar-request` header, the server looks for an SSE file first. Otherwise it serves HTML.

### Templates

Every file is a Go `html/template` with optional YAML frontmatter:

```html
---
status: 200
loop: true
interval: 2000
---
<div id="clock">Hits: {{.GlobalHits}}</div>
```

**Frontmatter options:**

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `status` | int | 200 | HTTP status code |
| `loop` | bool | false | Repeat SSE responses continuously |
| `interval` | int | 0 | Delay between loops in ms (SSE only) |
| `count` | int | 0 | Number of loops before advancing to the next sequential file (0 = infinite, SSE only) |
| `delay` | int | 5000 | Delay in ms between sequential SSE sections |

**Template variables:**

| Variable | Description |
|----------|-------------|
| `{{.Username}}` | Random session username (e.g. "clever-fox-42") |
| `{{.SessionID}}` | Session identifier |
| `{{.GlobalHits}}` | Total hits across all URLs |
| `{{.URLHits}}` | Hits to this URL |
| `{{.SessionURLHits}}` | Hits to this URL from this session |
| `{{.URL}}` | Current request path |
| `{{.Method}}` | HTTP method |
| `{{.Signals}}` | Datastar signals from the request |

**Template functions:**

The standard Go template functions are available in templates, plus all functions included in [Slim-Sprig](https://sprig.taskfile.dev). For more details, see the slim-sprig docs.

### Multiple Responses in One File

Separate sections with `===` to send multiple SSE fragments in a single request:

```html
---
loop: false
---
<div id="counter">Count: 1</div>
===
<div id="counter">Count: 2</div>
===
<div id="counter">Count: 3</div>
```

### Sequential Files

Numbered files progress per-session. The first request gets `sse_001.html`, the second gets `sse_002.html`, and so on:

```
steps/
├── sse_001.html  → first request
├── sse_002.html  → second request
└── sse_003.html  → third request
```

After the last file, the sequence stops (or loops if `loop: true`).

Use `count` to loop a file a fixed number of times before advancing to the next one in the sequence:

```html
<!-- steps/sse_001.html — loops 3 times, then advances to sse_002 -->
---
loop: true
interval: 1000
count: 3
---
<div id="progress">Loading...</div>
```

```html
<!-- steps/sse_002.html — sent once, then the connection closes -->
---
---
<div id="progress">Done!</div>
```

### Real-Time Messaging (NATS)

An embedded NATS server connects HTML handlers to SSE listeners. When a POST handler completes, its signals are automatically published to the session's NATS subject, triggering re-renders on any listening SSE connections. This is how the skeleton demo's "Send" button pushes messages to the live updates section without a page reload.

## Command Reference

### `dsplay`

Serve the current directory on port 8080 (shortcut for `dsplay serve .`).

### `dsplay init [directory]`

Create a skeleton playground. Omit the directory to scaffold in the current folder.

```bash
dsplay init                    # scaffold in current directory
dsplay init my-playground      # create my-playground/
dsplay init --force existing/  # overwrite existing files
```

### `dsplay serve [source]`

Serve a playground from a local directory or a GitHub Gist.

```bash
dsplay serve                                    # serve current directory
dsplay serve ./my-playground                    # serve a local directory
dsplay serve https://gist.github.com/user/id   # serve from a gist
dsplay serve --clone <gist-url>                 # clone gist to disk, then serve
```

### `dsplay share`

Publish the current directory as a GitHub Gist.

```bash
dsplay share --description "Demo playground"
dsplay share --secret                           # create a secret gist (default is public)
dsplay share --dir ./other-playground           # share a different directory
```

### Global Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--port` | 8080 | Port to listen on |
| `--secret` | dev secret | Session cookie secret |
| `--github-token` | — | GitHub token (or set `GITHUB_TOKEN`) |
| `--debug` | false | Enable debug logging |

## License

MIT
