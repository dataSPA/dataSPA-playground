package gist

import "testing"

func TestEncodePath(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"home/index.html", "home__index.html"},
		{"home/greeting/sse.html", "home__greeting__sse.html"},
		{"home/counter/live/sse.html", "home__counter__live__sse.html"},
		{"index.html", "index.html"},
	}
	for _, tt := range tests {
		got := EncodePath(tt.input)
		if got != tt.want {
			t.Errorf("EncodePath(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestDecodePath(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"home__index.html", "home/index.html"},
		{"home__greeting__sse.html", "home/greeting/sse.html"},
		{"home__counter__live__sse.html", "home/counter/live/sse.html"},
		{"index.html", "index.html"},
	}
	for _, tt := range tests {
		got := DecodePath(tt.input)
		if got != tt.want {
			t.Errorf("DecodePath(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestRoundTrip(t *testing.T) {
	paths := []string{
		"home/index.html",
		"home/greeting/sse.html",
		"home/counter/step/sse.html",
		"home/action/post.html",
	}
	for _, p := range paths {
		got := DecodePath(EncodePath(p))
		if got != p {
			t.Errorf("roundtrip(%q) = %q", p, got)
		}
	}
}
