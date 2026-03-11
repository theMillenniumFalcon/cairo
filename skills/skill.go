// Package skills loads user-defined skill YAML files from ~/.cairo/skills/
// and registers each one as a Tool in the registry.
//
// Skill YAML format:
//
//	name: summarize
//	description: Summarize the provided text concisely
//	prompt: "Summarize the following in 3 bullet points:\n{{.Input}}"
//
// Or wrapping a shell command:
//
//	name: count_lines
//	description: Count the number of lines in a file
//	command: "wc -l {{.Input}}"
//
// Both prompt and command can be combined:
// if command is set, it is executed and its output becomes {{.Output}},
// which can be referenced in prompt for a follow-up LLM pass.
package skills

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"text/template"
	"time"

	"github.com/themillenniumfalcon/cairo/llm"
)

// Skill is a user-defined capability loaded from a YAML file.
type Skill struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	// Prompt is a Go template rendered with SkillInput before sending to the LLM.
	// Leave empty to skip the LLM step.
	Prompt string `yaml:"prompt"`
	// Command is a Go template rendered with SkillInput and run as a shell command.
	// Leave empty to skip the shell step.
	Command string `yaml:"command"`
}

// SkillTool wraps a Skill so it satisfies tools.Tool.
type SkillTool struct {
	skill    Skill
	provider llm.Provider
}

func (s *SkillTool) Name() string        { return s.skill.Name }
func (s *SkillTool) Description() string { return s.skill.Description }

func (s *SkillTool) Run(input string) (string, error) {
	data := map[string]string{
		"Input":  input,
		"Output": "",
	}

	// Step 1: run command if defined
	if s.skill.Command != "" {
		cmd, err := renderTemplate(s.skill.Command, data)
		if err != nil {
			return "", fmt.Errorf("skill %s: render command: %w", s.skill.Name, err)
		}
		out, err := runShell(cmd)
		if err != nil {
			return "", fmt.Errorf("skill %s: command failed: %w", s.skill.Name, err)
		}
		data["Output"] = out

		// If no prompt, return command output directly
		if s.skill.Prompt == "" {
			return out, nil
		}
	}

	// Step 2: run LLM prompt if defined
	if s.skill.Prompt != "" {
		if s.provider == nil {
			return "", fmt.Errorf("skill %s: no LLM provider available", s.skill.Name)
		}
		prompt, err := renderTemplate(s.skill.Prompt, data)
		if err != nil {
			return "", fmt.Errorf("skill %s: render prompt: %w", s.skill.Name, err)
		}
		msgs := []llm.Message{
			{Role: llm.RoleUser, Content: prompt},
		}
		reply, err := s.provider.Chat(context.Background(), msgs)
		if err != nil {
			return "", fmt.Errorf("skill %s: llm: %w", s.skill.Name, err)
		}
		return reply, nil
	}

	return "", fmt.Errorf("skill %s: must define at least one of 'prompt' or 'command'", s.skill.Name)
}

// ── loader ────────────────────────────────────────────────────────────────────

// DefaultSkillsDir returns ~/.cairo/skills
func DefaultSkillsDir() string {
	return filepath.Join(".cairo", "skills")
}

// LoadDir reads all *.yaml / *.yml files from dir and returns parsed Skills.
// Missing dir is not an error.
func LoadDir(dir string) ([]Skill, error) {
	if dir == "" {
		dir = DefaultSkillsDir()
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("skills: read dir %s: %w", dir, err)
	}

	var out []Skill
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			continue
		}
		path := filepath.Join(dir, name)
		skill, err := parseSkillFile(path)
		if err != nil {
			return nil, fmt.Errorf("skills: %w", err)
		}
		out = append(out, skill)
	}
	return out, nil
}

// AsTools wraps skills as SkillTools, injecting the LLM provider.
func AsTools(skills []Skill, provider llm.Provider) []*SkillTool {
	out := make([]*SkillTool, len(skills))
	for i, s := range skills {
		out[i] = &SkillTool{skill: s, provider: provider}
	}
	return out
}

// ── YAML parser ───────────────────────────────────────────────────────────────
// Minimal: parses flat key: value and key: | multiline blocks.

func parseSkillFile(path string) (Skill, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Skill{}, fmt.Errorf("read %s: %w", path, err)
	}

	skill, err := parseSkillYAML(string(data))
	if err != nil {
		return Skill{}, fmt.Errorf("parse %s: %w", path, err)
	}

	if skill.Name == "" {
		// default name from filename without extension
		base := filepath.Base(path)
		skill.Name = strings.TrimSuffix(strings.TrimSuffix(base, ".yaml"), ".yml")
	}
	if skill.Description == "" {
		return Skill{}, fmt.Errorf("skill %q in %s: missing 'description'", skill.Name, path)
	}
	if skill.Prompt == "" && skill.Command == "" {
		return Skill{}, fmt.Errorf("skill %q in %s: must define 'prompt' or 'command'", skill.Name, path)
	}

	return skill, nil
}

// parseSkillYAML parses the simple YAML structure skills use.
// Supports:
//   - Plain scalar:  key: value
//   - Quoted scalar: key: "value with spaces"
//   - Block literal: key: |\n  line1\n  line2
//   - Block folded:  key: >\n  line1\n  line2  (newlines become spaces)
func parseSkillYAML(content string) (Skill, error) {
	var skill Skill
	scanner := bufio.NewScanner(strings.NewReader(content))

	type blockState int
	const (
		none    blockState = iota
		literal            // |
		folded             // >
	)

	var (
		currentKey  string
		blockMode   blockState
		blockLines  []string
		blockIndent int
	)

	flushBlock := func() {
		if currentKey == "" {
			return
		}
		var val string
		if blockMode == folded {
			val = strings.Join(blockLines, " ")
		} else {
			val = strings.Join(blockLines, "\n")
		}
		val = strings.TrimRight(val, "\n ")
		setSkillField(&skill, currentKey, val)
		currentKey = ""
		blockLines = nil
		blockMode = none
		blockIndent = 0
	}

	for scanner.Scan() {
		line := scanner.Text()
		raw := line

		// Strip comments only on non-block lines
		if blockMode == none {
			if idx := strings.Index(line, " #"); idx >= 0 {
				line = line[:idx]
			}
			line = strings.TrimRight(line, " \t")
			if line == "" {
				continue
			}
		}

		trimmed := strings.TrimLeft(raw, " \t")
		indent := len(raw) - len(trimmed)

		// Inside a block: collect indented lines
		if blockMode != none {
			if blockIndent == 0 && strings.TrimSpace(raw) != "" {
				blockIndent = indent
			}
			if indent >= blockIndent || strings.TrimSpace(raw) == "" {
				// strip the block indent
				stripped := raw
				if len(raw) >= blockIndent {
					stripped = raw[blockIndent:]
				}
				blockLines = append(blockLines, stripped)
				continue
			}
			// dedented — block ended
			flushBlock()
		}

		// Top-level key: value
		idx := strings.Index(line, ":")
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])

		switch val {
		case "|":
			flushBlock()
			currentKey = key
			blockMode = literal
		case ">":
			flushBlock()
			currentKey = key
			blockMode = folded
		default:
			flushBlock()
			// strip surrounding quotes
			val = strings.Trim(val, `"'`)
			setSkillField(&skill, key, val)
		}
	}
	flushBlock()

	return skill, scanner.Err()
}

func setSkillField(s *Skill, key, val string) {
	switch key {
	case "name":
		s.Name = val
	case "description":
		s.Description = val
	case "prompt":
		s.Prompt = val
	case "command":
		s.Command = val
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

func renderTemplate(tmpl string, data map[string]string) (string, error) {
	t, err := template.New("").Parse(tmpl)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func runShell(cmd string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var c *exec.Cmd
	if runtime.GOOS == "windows" {
		c = exec.CommandContext(ctx, "cmd", "/C", cmd)
	} else {
		c = exec.CommandContext(ctx, "sh", "-c", cmd)
	}

	var buf bytes.Buffer
	c.Stdout = &buf
	c.Stderr = &buf
	_ = c.Run()

	out := strings.TrimRight(buf.String(), "\n")
	if out == "" {
		return "(no output)", nil
	}
	return out, nil
}

// ── test helpers (exported for black-box testing) ─────────────────────────────

// ParseSkillYAMLTest exposes parseSkillYAML for testing.
func ParseSkillYAMLTest(content string) (Skill, error) {
	return parseSkillYAML(content)
}

// NewSkillToolTest exposes SkillTool construction for testing.
func NewSkillToolTest(s Skill, p llm.Provider) *SkillTool {
	return &SkillTool{skill: s, provider: p}
}
