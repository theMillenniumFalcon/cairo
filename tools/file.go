package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const maxReadBytes = 32768

// ReadFile reads the contents of a file.
type ReadFile struct{}

func (ReadFile) Name() string { return "read_file" }
func (ReadFile) Description() string {
	return "Read the contents of a file. Input is the file path."
}

func (ReadFile) Run(input string) (string, error) {
	path := strings.TrimSpace(input)
	if path == "" {
		return "", fmt.Errorf("no path provided")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read_file: %w", err)
	}

	out := string(data)
	if len(out) > maxReadBytes {
		out = out[:maxReadBytes] + "\n[file truncated]"
	}
	return out, nil
}

// WriteFile writes content to a file (creates or overwrites).
type WriteFile struct{}

func (WriteFile) Name() string { return "write_file" }
func (WriteFile) Description() string {
	return `Write content to a file. Input format: first line is the file path, remaining lines are the content.
Example:
/tmp/hello.txt
Hello, world!`
}

func (WriteFile) Run(input string) (string, error) {
	input = strings.TrimSpace(input)
	idx := strings.Index(input, "\n")
	if idx < 0 {
		return "", fmt.Errorf("write_file: input must be 'path\\ncontent'")
	}

	path := strings.TrimSpace(input[:idx])
	content := input[idx+1:]

	if path == "" {
		return "", fmt.Errorf("write_file: empty path")
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return "", fmt.Errorf("write_file: mkdir: %w", err)
	}

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("write_file: %w", err)
	}

	return fmt.Sprintf("wrote %d bytes to %s", len(content), path), nil
}

// ListDir lists files in a directory.
type ListDir struct{}

func (ListDir) Name() string { return "list_dir" }
func (ListDir) Description() string {
	return "List files and directories at a path. Input is the directory path (default: current directory)."
}

func (ListDir) Run(input string) (string, error) {
	path := strings.TrimSpace(input)
	if path == "" {
		path = "."
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return "", fmt.Errorf("list_dir: %w", err)
	}

	if len(entries) == 0 {
		return "(empty directory)", nil
	}

	var sb strings.Builder
	for _, e := range entries {
		info, _ := e.Info()
		if e.IsDir() {
			sb.WriteString(fmt.Sprintf("[dir]  %s\n", e.Name()))
		} else {
			size := int64(0)
			if info != nil {
				size = info.Size()
			}
			sb.WriteString(fmt.Sprintf("[file] %s  (%d bytes)\n", e.Name(), size))
		}
	}
	return strings.TrimRight(sb.String(), "\n"), nil
}
