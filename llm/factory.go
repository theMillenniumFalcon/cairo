package llm

import (
	"fmt"

	"github.com/themillenniumfalcon/cairo/config"
)

// New returns the Provider for the given provider name using cfg.
// If providerName is empty, cfg.DefaultProvider is used.
// If model is empty, the model from cfg is used.
func New(cfg *config.Config, providerName, model string) (Provider, error) {
	if providerName == "" {
		providerName = cfg.DefaultProvider
	}

	switch providerName {
	case "openai":
		m := cfg.OpenAI.Model
		if model != "" {
			m = model
		}
		return NewOpenAI(cfg.OpenAI.APIKey, m)

	case "anthropic":
		m := cfg.Anthropic.Model
		if model != "" {
			m = model
		}
		return NewAnthropic(cfg.Anthropic.APIKey, m)

	case "gemini":
		m := cfg.Gemini.Model
		if model != "" {
			m = model
		}
		return NewGemini(cfg.Gemini.APIKey, m)

	default:
		return nil, fmt.Errorf("unknown provider %q — choose: openai, anthropic, gemini", providerName)
	}
}
