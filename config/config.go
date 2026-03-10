package config

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

type Config struct {
	LLM LLMConfig
}

type LLMConfig struct {
	Provider        string
	AnthropicAPIKey string
	GeminiAPIKey    string
	OpenAIAPIKey    string
	Model           string
	OpenAIBaseURL   string // overrides the OpenAI API endpoint.
}

func Load() (*Config, error) {
	v := viper.New()

	// Defaults
	v.SetDefault("llm.provider", "anthropic")
	v.SetDefault("llm.model", "")

	// Config File
	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath("$HOME/.cairo")
	v.AddConfigPath(".")

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("config: read config file: %w", err)
		}
	}

	// Env vars
	// CAIRO_LLM_PROVIDER, CAIRO_LLM_MODEL, CAIRO_ANTHROPIC_API_KEY etc.
	v.SetEnvPrefix("CAIRO")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	if v.GetString("llm.anthropic_api_key") == "" {
		v.BindEnv("llm.anthropic_api_key", "ANTHROPIC_API_KEY")
	}
	if v.GetString("llm.gemini_api_key") == "" {
		v.BindEnv("llm.gemini_api_key", "GEMINI_API_KEY")
	}
	if v.GetString("llm.openai_api_key") == "" {
		v.BindEnv("llm.openai_api_key", "OPENAI_API_KEY")
	}

	cfg := &Config{
		LLM: LLMConfig{
			Provider:        v.GetString("llm.provider"),
			AnthropicAPIKey: v.GetString("llm.anthropic_api_key"),
			GeminiAPIKey:    v.GetString("llm.gemini_api_key"),
			OpenAIAPIKey:    v.GetString("llm.openai_api_key"),
			Model:           v.GetString("llm.model"),
			OpenAIBaseURL:   v.GetString("llm.openai_base_url"),
		},
	}

	return cfg, nil
}

func (c *Config) ActiveAPIKey() string {
	switch c.LLM.Provider {
	case "gemini":
		return c.LLM.GeminiAPIKey
	case "openai":
		return c.LLM.OpenAIAPIKey
	default:
		return c.LLM.GeminiAPIKey
	}
}
