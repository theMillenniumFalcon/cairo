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

const openAIBaseURL = "https://api.openai.com/v1/chat/completions"

type OpenAIProvider struct {
	apiKey string
	model  string
	client *http.Client
}

func NewOpenAI(apiKey, model string) (*OpenAIProvider, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("openai: api_key is required (set OPENAI_API_KEY in .env)")
	}
	return &OpenAIProvider{
		apiKey: apiKey,
		model:  model,
		client: &http.Client{},
	}, nil
}

func (p *OpenAIProvider) Name() string  { return "openai" }
func (p *OpenAIProvider) Model() string { return p.model }

func (p *OpenAIProvider) Chat(ctx context.Context, messages []Message) (string, error) {
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
		return "", fmt.Errorf("openai: request failed: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("openai: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("openai: API error %d: %s", resp.StatusCode, string(data))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("openai: parse response: %w", err)
	}
	if result.Error != nil {
		return "", fmt.Errorf("openai: %s", result.Error.Message)
	}
	if len(result.Choices) == 0 {
		return "", fmt.Errorf("openai: empty response")
	}
	return result.Choices[0].Message.Content, nil
}

func (p *OpenAIProvider) Stream(ctx context.Context, messages []Message, onToken func(string)) error {
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
		return fmt.Errorf("openai: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("openai: API error %d: %s", resp.StatusCode, string(data))
	}

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
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}
		if len(event.Choices) > 0 && event.Choices[0].Delta.Content != "" {
			onToken(event.Choices[0].Delta.Content)
		}
	}
	return scanner.Err()
}

func (p *OpenAIProvider) buildRequest(messages []Message, stream bool) ([]byte, error) {
	type reqMessage struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	type requestBody struct {
		Model    string       `json:"model"`
		Stream   bool         `json:"stream,omitempty"`
		Messages []reqMessage `json:"messages"`
	}

	msgs := make([]reqMessage, len(messages))
	for i, m := range messages {
		msgs[i] = reqMessage{Role: m.Role, Content: m.Content}
	}
	return json.Marshal(requestBody{Model: p.model, Stream: stream, Messages: msgs})
}

func (p *OpenAIProvider) newRequest(ctx context.Context, body []byte) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, openAIBaseURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("openai: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	return req, nil
}
