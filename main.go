package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/themillenniumfalcon/cairo/chat"
	"github.com/themillenniumfalcon/cairo/config"
	"github.com/themillenniumfalcon/cairo/llm"
)

const version = "0.1.0"

const usage = `Cairo — personal AI agent

USAGE:
  cairo [flags] [message]

  If a message is provided, Cairo responds and exits (one-shot mode).
  Without a message, Cairo starts an interactive chat session.

FLAGS:
  -provider  string   LLM provider: openai | anthropic | gemini (default: from config)
  -model     string   Model override (default: from config)
  -config    string   Path to config file (default: ~/.cairo/config.yaml)
  -version           Print version and exit

SETUP:
  Run 'cairo init' to create a default config file at ~/.cairo/config.yaml

EXAMPLES:
  cairo                              # start interactive chat
  cairo "explain recursion"          # one-shot query
  cairo -provider openai "hello"     # use specific provider
  cairo -provider gemini -model gemini-2.0-flash "hi"
`

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "cairo: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		providerFlag = flag.String("provider", "", "LLM provider: openai | anthropic | gemini")
		modelFlag    = flag.String("model", "", "Model override")
		configFlag   = flag.String("config", "", "Path to config file")
		versionFlag  = flag.Bool("version", false, "Print version")
	)

	flag.Usage = func() { fmt.Print(usage) }
	flag.Parse()

	if *versionFlag {
		fmt.Printf("cairo %s\n", version)
		return nil
	}

	// Subcommands
	args := flag.Args()
	if len(args) > 0 && args[0] == "init" {
		if err := config.WriteExample(); err != nil {
			return fmt.Errorf("init: %w", err)
		}
		fmt.Printf("Created config at %s\nAdd your API keys and run 'cairo' to start chatting.\n", config.DefaultConfigPath())
		return nil
	}

	// Load config
	cfg, err := config.Load(*configFlag)
	if err != nil {
		return err
	}

	// Build provider
	provider, err := llm.New(cfg, *providerFlag, *modelFlag)
	if err != nil {
		return err
	}

	// One-shot mode: cairo "some message"
	if len(args) > 0 {
		input := strings.Join(args, " ")
		messages := []llm.Message{
			{Role: llm.RoleSystem, Content: "You are Cairo, a personal AI agent. Be concise and direct."},
			{Role: llm.RoleUser, Content: input},
		}
		reply, err := provider.Chat(context.Background(), messages)
		if err != nil {
			return err
		}
		fmt.Println(reply)
		return nil
	}

	// Interactive mode
	return chat.CLI(provider)
}
