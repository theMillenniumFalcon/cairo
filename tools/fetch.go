package tools

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const fetchTimeout = 15 * time.Second
const maxFetchBytes = 16384

// Fetch performs an HTTP GET and returns the response body as plain text.
type Fetch struct {
	client *http.Client
}

func NewFetch() Fetch {
	return Fetch{client: &http.Client{Timeout: fetchTimeout}}
}

func (Fetch) Name() string { return "fetch" }
func (Fetch) Description() string {
	return "Fetch the content of a URL via HTTP GET. Input is the URL."
}

func (f Fetch) Run(input string) (string, error) {
	url := strings.TrimSpace(input)
	if url == "" {
		return "", fmt.Errorf("no URL provided")
	}
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		url = "https://" + url
	}

	resp, err := f.client.Get(url)
	if err != nil {
		return "", fmt.Errorf("fetch: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxFetchBytes))
	if err != nil {
		return "", fmt.Errorf("fetch: read body: %w", err)
	}

	out := string(body)

	// Strip HTML tags for readability
	out = stripHTML(out)
	out = strings.TrimSpace(out)

	if len(body) >= maxFetchBytes {
		out += "\n[content truncated]"
	}
	if out == "" {
		return fmt.Sprintf("(empty response, status %d)", resp.StatusCode), nil
	}
	return fmt.Sprintf("[status %d]\n%s", resp.StatusCode, out), nil
}

// stripHTML removes HTML tags and collapses whitespace.
func stripHTML(s string) string {
	var b strings.Builder
	inTag := false
	for _, r := range s {
		switch {
		case r == '<':
			inTag = true
			b.WriteRune(' ')
		case r == '>':
			inTag = false
		case !inTag:
			b.WriteRune(r)
		}
	}
	// Collapse runs of whitespace
	out := b.String()
	lines := strings.Split(out, "\n")
	var result []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			result = append(result, line)
		}
	}
	return strings.Join(result, "\n")
}
