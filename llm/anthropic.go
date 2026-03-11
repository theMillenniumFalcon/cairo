package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const anthropicBaseURL = "https://api.anthropic.com/v1/messages"
const anthropicVersion = "2023-06-01"

type AnthropicProvider struct {
	apiKey string
	model  string
	client *http.Client
}

func NewAnthropic(apiKey, model string) (*AnthropicProvider, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("anthropic: api_key is required (set ANTHROPIC_API_KEY in .env)")
	}
	return &AnthropicProvider{
		apiKey: apiKey,
		model:  model,
		client: &http.Client{},
	}, nil
}

func (p *AnthropicProvider) Name() string  { return "anthropic" }
func (p *AnthropicProvider) Model() string { return p.model }

func (p *AnthropicProvider) Chat(ctx context.Context, messages []Message) (string, error) {
	body, err := p.buildRequest(messages, false)
	if err != nil {
		return "", err
	}

	req, err := p.newRequest(ctx, body)
	if err != nil {
		return "", err
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("anthropic: request failed: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("anthropic: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("anthropic: API error %d: %s", resp.StatusCode, string(data))
	}

	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("anthropic: parse response: %w", err)
	}
	if result.Error != nil {
		return "", fmt.Errorf("anthropic: %s", result.Error.Message)
	}
	for _, block := range result.Content {
		if block.Type == "text" {
			return block.Text, nil
		}
	}
	return "", fmt.Errorf("anthropic: empty response")
}

func (p *AnthropicProvider) Stream(ctx context.Context, messages []Message, onToken func(string)) error {
	body, err := p.buildRequest(messages, true)
	if err != nil {
		return err
	}

	req, err := p.newRequest(ctx, body)
	if err != nil {
		return err
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("anthropic: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("anthropic: API error %d: %s", resp.StatusCode, string(data))
	}

	// Parse SSE stream
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}
		var event struct {
			Type  string `json:"type"`
			Delta struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"delta"`
		}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}
		if event.Type == "content_block_delta" && event.Delta.Type == "text_delta" {
			onToken(event.Delta.Text)
		}
	}
	return scanner.Err()
}

func (p *AnthropicProvider) buildRequest(messages []Message, stream bool) ([]byte, error) {
	type reqMessage struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	type requestBody struct {
		Model     string       `json:"model"`
		MaxTokens int          `json:"max_tokens"`
		Stream    bool         `json:"stream,omitempty"`
		System    string       `json:"system,omitempty"`
		Messages  []reqMessage `json:"messages"`
	}

	var systemPrompt string
	var msgs []reqMessage
	for _, m := range messages {
		if m.Role == RoleSystem {
			systemPrompt = m.Content
			continue
		}
		msgs = append(msgs, reqMessage{Role: m.Role, Content: m.Content})
	}

	return json.Marshal(requestBody{
		Model:     p.model,
		MaxTokens: 8096,
		Stream:    stream,
		System:    systemPrompt,
		Messages:  msgs,
	})
}

func (p *AnthropicProvider) newRequest(ctx context.Context, body []byte) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, anthropicBaseURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("anthropic: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("anthropic-version", anthropicVersion)
	return req, nil
}
