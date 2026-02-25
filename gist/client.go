package gist

import (
	"context"

	"github.com/google/go-github/v68/github"
	githubauth "github.com/jferrl/go-githubauth"
	"golang.org/x/oauth2"
)

// Client wraps a GitHub API client for gist operations.
type Client struct {
	gh    *github.Client
	token string // raw token, used for authenticated git clone URLs
}

// NewClient creates a new gist Client. If token is empty, the client is
// unauthenticated (only public gist reads will work).
func NewClient(token string) *Client {
	if token == "" {
		return &Client{gh: github.NewClient(nil)}
	}

	tokenSource := githubauth.NewPersonalAccessTokenSource(token)
	httpClient := oauth2.NewClient(context.Background(), tokenSource)
	return &Client{
		gh:    github.NewClient(httpClient),
		token: token,
	}
}
