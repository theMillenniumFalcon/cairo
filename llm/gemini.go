package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const geminiBaseURL = "https://generativelanguage.googleapis.com/v1beta/models"

type GeminiProvider struct {
	apiKey string
	model  string
	client *http.Client
}

func NewGemini(apiKey, model string) (*GeminiProvider, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("gemini: api_key is required (set in config or GEMINI_API_KEY)")
	}
	return &GeminiProvider{
		apiKey: apiKey,
		model:  model,
		client: &http.Client{},
	}, nil
}

func (p *GeminiProvider) Name() string  { return "gemini" }
func (p *GeminiProvider) Model() string { return p.model }

func (p *GeminiProvider) Chat(ctx context.Context, messages []Message) (string, error) {
	// Gemini API structure
	type part struct {
		Text string `json:"text"`
	}
	type content struct {
		Role  string `json:"role"`
		Parts []part `json:"parts"`
	}
	type systemInstruction struct {
		Parts []part `json:"parts"`
	}
	type requestBody struct {
		SystemInstruction *systemInstruction `json:"system_instruction,omitempty"`
		Contents          []content          `json:"contents"`
	}

	var body requestBody
	for _, m := range messages {
		switch m.Role {
		case RoleSystem:
			body.SystemInstruction = &systemInstruction{
				Parts: []part{{Text: m.Content}},
			}
		case RoleUser:
			body.Contents = append(body.Contents, content{
				Role:  "user",
				Parts: []part{{Text: m.Content}},
			})
		case RoleAssistant:
			// Gemini uses "model" for assistant role
			body.Contents = append(body.Contents, content{
				Role:  "model",
				Parts: []part{{Text: m.Content}},
			})
		}
	}

	reqData, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("gemini: marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/%s:generateContent?key=%s", geminiBaseURL, p.model, p.apiKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(reqData))
	if err != nil {
		return "", fmt.Errorf("gemini: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("gemini: request failed: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("gemini: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("gemini: API error %d: %s", resp.StatusCode, string(data))
	}

	// Parse response
	var result struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("gemini: parse response: %w", err)
	}
	if result.Error != nil {
		return "", fmt.Errorf("gemini: %s", result.Error.Message)
	}
	if len(result.Candidates) == 0 || len(result.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("gemini: empty response")
	}

	return result.Candidates[0].Content.Parts[0].Text, nil
}
