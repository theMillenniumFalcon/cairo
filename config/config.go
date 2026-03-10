package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	DefaultProvider string

	OpenAI struct {
		APIKey string
		Model  string
	}

	Anthropic struct {
		APIKey string
		Model  string
	}

	Gemini struct {
		APIKey string
		Model  string
	}
}

func DefaultConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cairo", "config.yaml")
}

// Load reads the config file. Missing file is not an error — env vars still work.
func Load(path string) (*Config, error) {
	if path == "" {
		path = DefaultConfigPath()
	}

	cfg := &Config{}
	setDefaults(cfg)

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			applyEnvOverrides(cfg)
			return cfg, nil
		}
		return nil, fmt.Errorf("reading config: %w", err)
	}

	if err := parseYAML(cfg, string(data)); err != nil {
		return nil, fmt.Errorf("parsing config %s: %w", path, err)
	}

	applyEnvOverrides(cfg)
	return cfg, nil
}

// WriteExample creates a default config file at ~/.cairo/config.yaml.
func WriteExample() error {
	path := DefaultConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(ExampleConfig()), 0600)
}

func ExampleConfig() string {
	return `# Cairo configuration
default_provider: anthropic

openai:
  api_key: ""
  model: gpt-4o

anthropic:
  api_key: ""
  model: claude-sonnet-4-20250514

gemini:
  api_key: ""
  model: gemini-2.0-flash
`
}

func setDefaults(cfg *Config) {
	cfg.DefaultProvider = "anthropic"
	cfg.OpenAI.Model = "gpt-4o"
	cfg.Anthropic.Model = "claude-sonnet-4-20250514"
	cfg.Gemini.Model = "gemini-2.0-flash"
}

func applyEnvOverrides(cfg *Config) {
	if key := os.Getenv("OPENAI_API_KEY"); key != "" {
		cfg.OpenAI.APIKey = key
	}
	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		cfg.Anthropic.APIKey = key
	}
	if key := os.Getenv("GEMINI_API_KEY"); key != "" {
		cfg.Gemini.APIKey = key
	}
	if p := os.Getenv("CAIRO_PROVIDER"); p != "" {
		cfg.DefaultProvider = p
	}
}

// parseYAML handles the simple two-level YAML Cairo uses.
func parseYAML(cfg *Config, content string) error {
	scanner := bufio.NewScanner(strings.NewReader(content))
	var section string

	for scanner.Scan() {
		line := scanner.Text()
		if idx := strings.Index(line, "#"); idx >= 0 {
			line = line[:idx]
		}
		line = strings.TrimRight(line, " \t")
		if line == "" {
			continue
		}

		trimmed := strings.TrimLeft(line, " ")
		indent := len(line) - len(trimmed)

		k, v := splitKV(trimmed)

		if indent == 0 {
			if v != "" {
				if k == "default_provider" {
					cfg.DefaultProvider = v
				}
			} else {
				section = k
			}
			continue
		}

		switch section {
		case "openai":
			switch k {
			case "api_key":
				cfg.OpenAI.APIKey = v
			case "model":
				if v != "" {
					cfg.OpenAI.Model = v
				}
			}
		case "anthropic":
			switch k {
			case "api_key":
				cfg.Anthropic.APIKey = v
			case "model":
				if v != "" {
					cfg.Anthropic.Model = v
				}
			}
		case "gemini":
			switch k {
			case "api_key":
				cfg.Gemini.APIKey = v
			case "model":
				if v != "" {
					cfg.Gemini.Model = v
				}
			}
		}
	}
	return scanner.Err()
}

func splitKV(s string) (string, string) {
	idx := strings.Index(s, ":")
	if idx < 0 {
		return strings.TrimSpace(s), ""
	}
	k := strings.TrimSpace(s[:idx])
	v := strings.Trim(strings.TrimSpace(s[idx+1:]), `"'`)
	return k, v
}
