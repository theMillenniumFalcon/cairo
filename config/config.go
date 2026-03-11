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

	Telegram struct {
		BotToken string
	}
}

func DefaultConfigPath() string {
	return filepath.Join(".cairo", "config.yaml")
}

// Load reads the config. Priority (highest to lowest):
//  1. Real environment variables (already set in shell)
//  2. .env file in the current working directory
//  3. config.yaml
//  4. Built-in defaults
func Load(path string) (*Config, error) {
	if path == "" {
		path = DefaultConfigPath()
	}

	cfg := &Config{}
	setDefaults(cfg)

	// 1. config.yaml (lowest priority after defaults)
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("reading config: %w", err)
	}
	if len(data) > 0 {
		if err := parseYAML(cfg, string(data)); err != nil {
			return nil, fmt.Errorf("parsing config %s: %w", path, err)
		}
	}

	// 2. .env file in cwd (overrides config.yaml)
	if err := loadDotEnv(".env"); err != nil {
		return nil, fmt.Errorf(".env: %w", err)
	}

	// 3. Real env vars (highest priority)
	applyEnvOverrides(cfg)

	return cfg, nil
}

// loadDotEnv reads KEY=VALUE pairs from a file and sets them via os.Setenv
// only if the key is not already set in the environment.
func loadDotEnv(path string) error {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // .env is optional
		}
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// skip blanks and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// strip optional "export " prefix
		line = strings.TrimPrefix(line, "export ")

		idx := strings.Index(line, "=")
		if idx < 0 {
			continue // not a KEY=VALUE line, skip silently
		}

		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])

		// strip surrounding quotes: "value" or 'value'
		if len(val) >= 2 {
			if (val[0] == '"' && val[len(val)-1] == '"') ||
				(val[0] == '\'' && val[len(val)-1] == '\'') {
				val = val[1 : len(val)-1]
			}
		}

		// only set if not already present in the real environment
		if os.Getenv(key) == "" {
			os.Setenv(key, val)
		}
	}
	return scanner.Err()
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
# Tip: you can also put your API keys in a .env file next to your project.
default_provider: gemini

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
	cfg.DefaultProvider = "gemini"
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
	if t := os.Getenv("TELEGRAM_BOT_TOKEN"); t != "" {
		cfg.Telegram.BotToken = t
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
