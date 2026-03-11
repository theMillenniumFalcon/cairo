package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/themillenniumfalcon/cairo/agent"
	"github.com/themillenniumfalcon/cairo/chat"
	"github.com/themillenniumfalcon/cairo/config"
	"github.com/themillenniumfalcon/cairo/db"
	"github.com/themillenniumfalcon/cairo/llm"
	"github.com/themillenniumfalcon/cairo/skills"
	"github.com/themillenniumfalcon/cairo/tools"
)

const version = "0.5.0"

const usage = `Cairo — personal AI agent

USAGE:
  cairo [flags] [message]
  cairo <subcommand> [args]

  Without a message: starts an interactive chat session.
  With a message: one-shot query, then exits.

FLAGS:
  -provider     string   LLM provider: openai | anthropic | gemini (default: from config)
  -model        string   Model override (default: from config)
  -session      string   Session name to resume or create (default: "default")
  -config       string   Path to config file (default: ~/.cairo/config.yaml)
  -skills-dir   string   Skills directory (default: ~/.cairo/skills)
  -version              Print version and exit

SUBCOMMANDS:
  init                          Create default config at ~/.cairo/config.yaml
  telegram                      Start the Telegram bot (long-polling)
  sessions list                 List all sessions
  sessions delete <n>           Delete a session and its history
  sessions rename <old> <new>   Rename a session
  skills list                   List all loaded skills

BUILT-IN TOOLS:
  shell, read_file, write_file, list_dir, fetch

SKILLS (~/.cairo/skills/*.yaml):
  Add YAML files to define custom tools — no recompile needed.
  See skills/examples/ in the source for sample skill files.

EXAMPLES:
  cairo                                 # interactive chat
  cairo -session myproject              # named session
  cairo "summarize this for me: ..."    # one-shot
  cairo telegram                        # start Telegram bot
  cairo skills list                     # see loaded skills
`

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "cairo: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		providerFlag  = flag.String("provider", "", "LLM provider: openai | anthropic | gemini")
		modelFlag     = flag.String("model", "", "Model override")
		sessionFlag   = flag.String("session", "default", "Session name")
		configFlag    = flag.String("config", "", "Path to config file")
		skillsDirFlag = flag.String("skills-dir", "", "Skills directory")
		versionFlag   = flag.Bool("version", false, "Print version")
	)

	flag.Usage = func() { fmt.Print(usage) }
	flag.Parse()

	if *versionFlag {
		fmt.Printf("cairo %s\n", version)
		return nil
	}

	args := flag.Args()

	// init — no DB or provider needed
	if len(args) > 0 && args[0] == "init" {
		if err := config.WriteExample(); err != nil {
			return fmt.Errorf("init: %w", err)
		}
		fmt.Printf("Created config at %s\nAdd your API keys and run 'cairo' to start.\n",
			config.DefaultConfigPath())
		return nil
	}

	// Load config (reads .env, config.yaml, env vars)
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

	// sessions subcommand (no provider needed)
	if len(args) > 0 && args[0] == "sessions" {
		return runSessionsCmd(store, args[1:])
	}

	// Build LLM provider
	provider, err := llm.New(cfg, *providerFlag, *modelFlag)
	if err != nil {
		return err
	}

	// Build tool registry (built-ins + skills)
	registry, loadedSkills, err := buildRegistry(provider, *skillsDirFlag)
	if err != nil {
		return err
	}

	if len(loadedSkills) > 0 {
		log.Printf("skills: loaded %d skill(s): %s", len(loadedSkills), skillNames(loadedSkills))
	}

	// skills list subcommand
	if len(args) > 0 && args[0] == "skills" {
		return runSkillsCmd(registry, loadedSkills, args[1:])
	}

	// telegram subcommand
	if len(args) > 0 && args[0] == "telegram" {
		return runTelegram(cfg, provider, registry, store)
	}

	// Load or create session
	sess, _, err := agent.LoadOrCreate(store, registry, *sessionFlag, provider.Name(), provider.Model())
	if err != nil {
		return err
	}

	// One-shot mode: cairo [flags] "some message"
	if len(args) > 0 {
		input := strings.Join(args, " ")

		if err := sess.Add(llm.RoleUser, input); err != nil {
			return err
		}
		reply, err := agent.RunReAct(
			context.Background(),
			provider,
			registry,
			sess.History,
			func(step agent.Step) {
				fmt.Fprintf(os.Stderr, "  [%s] %s\n", step.Action, step.ActionInput)
			},
		)
		if err != nil {
			return err
		}
		fmt.Println(reply)
		return sess.Add(llm.RoleAssistant, reply)
	}

	// Interactive CLI mode
	return chat.CLI(provider, sess, registry)
}

// buildRegistry creates the tool registry with built-ins and any loaded skills.
func buildRegistry(provider llm.Provider, skillsDir string) (*tools.Registry, []skills.Skill, error) {
	r := tools.NewRegistry()

	// Built-in tools
	r.Register(tools.Shell{})
	r.Register(tools.ReadFile{})
	r.Register(tools.WriteFile{})
	r.Register(tools.ListDir{})
	r.Register(tools.NewFetch())

	// Load skills from disk
	loaded, err := skills.LoadDir(skillsDir)
	if err != nil {
		return nil, nil, fmt.Errorf("load skills: %w", err)
	}

	for _, st := range skills.AsTools(loaded, provider) {
		r.Register(st)
	}

	return r, loaded, nil
}

// runSkillsCmd handles: skills list
func runSkillsCmd(registry *tools.Registry, loaded []skills.Skill, args []string) error {
	sub := "list"
	if len(args) > 0 {
		sub = args[0]
	}

	switch sub {
	case "list":
		allTools := registry.All()
		builtins := 5 // shell, read_file, write_file, list_dir, fetch

		fmt.Printf("Built-in tools (%d):\n", builtins)
		for _, t := range allTools[:builtins] {
			fmt.Printf("  %-16s  %s\n", t.Name(), t.Description())
		}

		fmt.Printf("\nSkills (%d) from %s:\n", len(loaded), skills.DefaultSkillsDir())
		if len(loaded) == 0 {
			fmt.Println("  (none — add .yaml files to ~/.cairo/skills/)")
		} else {
			for _, t := range allTools[builtins:] {
				fmt.Printf("  %-16s  %s\n", t.Name(), t.Description())
			}
		}
	default:
		return fmt.Errorf("unknown skills subcommand %q — try: list", sub)
	}
	return nil
}

// runTelegram starts the Telegram bot and blocks until SIGINT/SIGTERM.
func runTelegram(cfg *config.Config, provider llm.Provider, registry *tools.Registry, store *db.DB) error {
	token := cfg.Telegram.BotToken
	if token == "" {
		return fmt.Errorf("telegram: TELEGRAM_BOT_TOKEN is not set\n\nAdd it to your .env:\n  TELEGRAM_BOT_TOKEN=your-token-here\n\nGet a token from @BotFather on Telegram.")
	}

	bot := chat.NewBot(token, provider, registry, store)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sig
		log.Println("cairo: shutting down…")
		cancel()
	}()

	return bot.Run(ctx)
}

// ── sessions subcommand ───────────────────────────────────────────────────────

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
			fmt.Printf("%-4d  %-22s  %-20s  %-19s  %d\n",
				s.ID,
				truncate(s.Name, 22),
				truncate(s.Provider+"/"+s.Model, 20),
				s.UpdatedAt.Format("2006-01-02 15:04:05"),
				count,
			)
		}
	case "delete":
		if len(args) < 2 {
			return fmt.Errorf("usage: cairo sessions delete <n>")
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

// ── helpers ───────────────────────────────────────────────────────────────────

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func skillNames(ss []skills.Skill) string {
	names := make([]string, len(ss))
	for i, s := range ss {
		names[i] = s.Name
	}
	return strings.Join(names, ", ")
}
