package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/themillenniumfalcon/cairo/agent"
	"github.com/themillenniumfalcon/cairo/chat"
	"github.com/themillenniumfalcon/cairo/config"
	"github.com/themillenniumfalcon/cairo/db"
	"github.com/themillenniumfalcon/cairo/llm"
)

const version = "0.2.0"

const usage = `Cairo — personal AI agent

USAGE:
  cairo [flags] [message]
  cairo <subcommand> [args]

  If a message is provided, Cairo responds and exits (one-shot mode).
  Without a message, Cairo starts an interactive chat session.

FLAGS:
  -provider  string   LLM provider: openai | anthropic | gemini (default: from config)
  -model     string   Model override (default: from config)
  -session   string   Session name to resume or create (default: "default")
  -config    string   Path to config file (default: ~/.cairo/config.yaml)
  -version           Print version and exit

SUBCOMMANDS:
  init                          Create default config at ~/.cairo/config.yaml
  sessions list                 List all sessions
  sessions delete <name>        Delete a session and its history
  sessions rename <old> <new>   Rename a session

EXAMPLES:
  cairo                                 # interactive chat (default session)
  cairo -session myproject              # resume or start named session
  cairo "explain recursion"             # one-shot query
  cairo -provider openai "hello"        # use specific provider
  cairo sessions list                   # see all sessions
  cairo sessions delete myproject       # delete a session
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
		sessionFlag  = flag.String("session", "default", "Session name")
		configFlag   = flag.String("config", "", "Path to config file")
		versionFlag  = flag.Bool("version", false, "Print version")
	)

	flag.Usage = func() { fmt.Print(usage) }
	flag.Parse()

	if *versionFlag {
		fmt.Printf("cairo %s\n", version)
		return nil
	}

	args := flag.Args()

	// init subcommand — needs no DB
	if len(args) > 0 && args[0] == "init" {
		if err := config.WriteExample(); err != nil {
			return fmt.Errorf("init: %w", err)
		}
		fmt.Printf("Created config at %s\nAdd your API keys and run 'cairo' to start chatting.\n",
			config.DefaultConfigPath())
		return nil
	}

	// Load config
	cfg, err := config.Load(*configFlag)
	if err != nil {
		return err
	}

	// Open DB
	store, err := db.Open("")
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer store.Close()

	// sessions subcommand
	if len(args) > 0 && args[0] == "sessions" {
		return runSessionsCmd(store, args[1:])
	}

	// Build LLM provider
	provider, err := llm.New(cfg, *providerFlag, *modelFlag)
	if err != nil {
		return err
	}

	// Load or create the session
	sess, _, err := agent.LoadOrCreate(store, *sessionFlag, provider.Name(), provider.Model())
	if err != nil {
		return err
	}

	// One-shot mode: cairo [flags] "some message"
	if len(args) > 0 {
		input := strings.Join(args, " ")

		if err := sess.Add(llm.RoleUser, input); err != nil {
			return err
		}
		reply, err := provider.Chat(context.Background(), sess.History)
		if err != nil {
			return err
		}
		fmt.Println(reply)
		return sess.Add(llm.RoleAssistant, reply)
	}

	// Interactive mode
	return chat.CLI(provider, sess)
}

func runSessionsCmd(store *db.DB, args []string) error {
	if len(args) == 0 {
		fmt.Println("Usage: cairo sessions <list|delete|rename> [args]")
		return nil
	}

	switch args[0] {
	case "list":
		sessions, err := store.ListSessions()
		if err != nil {
			return err
		}
		if len(sessions) == 0 {
			fmt.Println("No sessions yet.")
			return nil
		}
		fmt.Printf("%-4s  %-22s  %-20s  %-19s  %s\n", "ID", "NAME", "PROVIDER/MODEL", "UPDATED", "MSGS")
		fmt.Println(strings.Repeat("─", 76))
		for _, s := range sessions {
			count, _ := store.CountMessages(s.ID)
			provModel := s.Provider + "/" + s.Model
			fmt.Printf("%-4d  %-22s  %-20s  %-19s  %d\n",
				s.ID,
				truncate(s.Name, 22),
				truncate(provModel, 20),
				s.UpdatedAt.Format("2006-01-02 15:04:05"),
				count,
			)
		}

	case "delete":
		if len(args) < 2 {
			return fmt.Errorf("usage: cairo sessions delete <name>")
		}
		if err := store.DeleteSession(args[1]); err != nil {
			return err
		}
		fmt.Printf("Session %q deleted.\n", args[1])

	case "rename":
		if len(args) < 3 {
			return fmt.Errorf("usage: cairo sessions rename <old-name> <new-name>")
		}
		if err := store.RenameSession(args[1], args[2]); err != nil {
			return err
		}
		fmt.Printf("Session %q renamed to %q.\n", args[1], args[2])

	default:
		return fmt.Errorf("unknown sessions subcommand %q — try: list, delete, rename", args[0])
	}

	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
