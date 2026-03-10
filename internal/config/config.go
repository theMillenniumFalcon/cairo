package config

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config holds all Cairo configuration.
type Config struct {
	Default   DefaultConfig   `yaml:"default"`
	OpenAI    OpenAIConfig    `yaml:"openai"`
	Anthropic AnthropicConfig `yaml:"anthropic"`
	Gemini    GeminiConfig    `yaml:"gemini"`
	DBPath    string          `yaml:"db_path"`
	LogLevel  string          `yaml:"log_level"`
}

type DefaultConfig struct {
	Provider     string  `yaml:"provider"` // openai | anthropic | gemini
	Model        string  `yaml:"model"`
	SystemPrompt string  `yaml:"system_prompt"`
	MaxTokens    int     `yaml:"max_tokens"`
	Temperature  float64 `yaml:"temperature"`
}

type OpenAIConfig struct {
	APIKey  string `yaml:"api_key"`
	BaseURL string `yaml:"base_url"` // optional override
}

type AnthropicConfig struct {
	APIKey string `yaml:"api_key"`
}

type GeminiConfig struct {
	APIKey string `yaml:"api_key"`
}

// Load reads config from file and merges with environment variables.
// File path is optional; falls back to ~/.cairo/config.yaml.
func Load(path string) (*Config, error) {
	if path == "" {
		path = DefaultPath()
	}

	cfg := defaults()

	// Read file if it exists (not an error if missing)
	data, err := os.ReadFile(path)
	if err == nil {
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("parsing config %s: %w", path, err)
		}
	}

	// Environment variables override file values
	applyEnv(cfg)

	// Resolve DBPath
	if cfg.DBPath == "" {
		cfg.DBPath = filepath.Join(cairoDirBase(), "cairo.db")
	}
	cfg.DBPath = expandHome(cfg.DBPath)

	return cfg, nil
}

// defaults returns a Config with sensible defaults.
func defaults() *Config {
	return &Config{
		Default: DefaultConfig{
			Provider:    "openai",
			Model:       "gpt-4o",
			MaxTokens:   4096,
			Temperature: 0.7,
			SystemPrompt: "You are Cairo, a helpful personal AI agent. " +
				"You are concise, accurate, and proactive. " +
				"When given tasks, you break them down and execute them step by step.",
		},
		LogLevel: "info",
	}
}

// applyEnv reads well-known env vars and overrides config fields.
func applyEnv(cfg *Config) {
	if v := os.Getenv("OPENAI_API_KEY"); v != "" {
		cfg.OpenAI.APIKey = v
	}
	if v := os.Getenv("ANTHROPIC_API_KEY"); v != "" {
		cfg.Anthropic.APIKey = v
	}
	if v := os.Getenv("GEMINI_API_KEY"); v != "" {
		cfg.Gemini.APIKey = v
	}
	if v := os.Getenv("CAIRO_PROVIDER"); v != "" {
		cfg.Default.Provider = v
	}
	if v := os.Getenv("CAIRO_MODEL"); v != "" {
		cfg.Default.Model = v
	}
	if v := os.Getenv("CAIRO_DB_PATH"); v != "" {
		cfg.DBPath = v
	}
	if v := os.Getenv("OPENAI_BASE_URL"); v != "" {
		cfg.OpenAI.BaseURL = v
	}
}

// DefaultPath returns the default config file path.
func DefaultPath() string {
	return filepath.Join(cairoDirBase(), "config.yaml")
}

func cairoDirBase() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".cairo"
	}
	return filepath.Join(home, ".cairo")
}

func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

// EnsureDir creates ~/.cairo if it doesn't exist.
func EnsureDir() error {
	dir := cairoDirBase()
	return os.MkdirAll(dir, 0700)
}

// --- Key-based access for `cairo config set/get/list` ---

// SetKey writes a single dotted key to the YAML config file.
// e.g. SetKey("openai.api_key", "sk-...")
func SetKey(path, key, value string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}

	// Load existing raw map
	raw := map[string]interface{}{}
	if data, err := os.ReadFile(path); err == nil {
		_ = yaml.Unmarshal(data, &raw)
	}

	// Set nested key
	parts := strings.SplitN(key, ".", 2)
	if len(parts) == 1 {
		raw[key] = value
	} else {
		sub, _ := raw[parts[0]].(map[string]interface{})
		if sub == nil {
			sub = map[string]interface{}{}
		}
		sub[parts[1]] = value
		raw[parts[0]] = sub
	}

	data, err := yaml.Marshal(raw)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

type KeyValue struct {
	Key   string
	Value string
}

// GetKey retrieves a dotted key from a loaded Config using reflection.
func GetKey(cfg *Config, key string) (string, error) {
	v, err := getReflect(reflect.ValueOf(cfg).Elem(), strings.Split(key, "."))
	if err != nil {
		return "", fmt.Errorf("key %q not found", key)
	}
	return fmt.Sprintf("%v", v), nil
}

// ListKeys returns all config key=value pairs with secrets masked.
func ListKeys(cfg *Config) []KeyValue {
	var out []KeyValue
	flattenStruct(reflect.ValueOf(cfg).Elem(), "", &out)
	// Mask secrets
	for i := range out {
		k := strings.ToLower(out[i].Key)
		if strings.Contains(k, "api_key") || strings.Contains(k, "token") {
			if len(out[i].Value) > 8 {
				out[i].Value = out[i].Value[:4] + "****" + out[i].Value[len(out[i].Value)-4:]
			} else if out[i].Value != "" {
				out[i].Value = "****"
			}
		}
	}
	return out
}

func flattenStruct(v reflect.Value, prefix string, out *[]KeyValue) {
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		fv := v.Field(i)
		tag := field.Tag.Get("yaml")
		if tag == "" || tag == "-" {
			continue
		}
		name := tag
		if prefix != "" {
			name = prefix + "." + tag
		}
		if fv.Kind() == reflect.Struct {
			flattenStruct(fv, name, out)
		} else {
			*out = append(*out, KeyValue{Key: name, Value: fmt.Sprintf("%v", fv.Interface())})
		}
	}
}

func getReflect(v reflect.Value, parts []string) (interface{}, error) {
	if len(parts) == 0 {
		return v.Interface(), nil
	}
	if v.Kind() != reflect.Struct {
		return nil, fmt.Errorf("not a struct")
	}
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		tag := t.Field(i).Tag.Get("yaml")
		if tag == parts[0] {
			return getReflect(v.Field(i), parts[1:])
		}
	}
	return nil, fmt.Errorf("field not found")
}
