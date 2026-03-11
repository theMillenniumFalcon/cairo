# Cairo

Personal AI agent — single binary, zero dependencies, runs anywhere. Chat via CLI, automate with tools, extend with skills, and control via Telegram.

## Features

- **Multi-provider** — OpenAI, Anthropic, and Gemini with a single flag to switch
- **Streaming output** — responses print token by token, no waiting
- **Persistent sessions** — named sessions with full history saved locally (`.cairo/cairo.json`)
- **ReAct tool loop** — agent reasons, calls tools, observes results, and loops until done
- **Built-in tools** — `shell`, `read_file`, `write_file`, `list_dir`, `fetch`
- **Skills system** — define custom tools in YAML, no recompile needed
- **Telegram bot** — full agent over Telegram with per-chat sessions
- **Zero CGo** — pure Go, cross-compiles to Linux / macOS / Windows without a C toolchain

## Install

```bash
# From source
git clone https://github.com/yourusername/cairo
cd cairo
make build          # ./cairo
make install        # installs to GOPATH/bin
```

## Setup

Create a `.env` file in your project directory:

```env
GEMINI_API_KEY=your-key-here
ANTHROPIC_API_KEY=sk-ant-...
OPENAI_API_KEY=sk-...

CAIRO_PROVIDER=gemini       # default provider
TELEGRAM_BOT_TOKEN=...      # optional, for Telegram bot
```

Or generate a config file:

```bash
cairo init    # creates .cairo/config.yaml
```

## Usage

```bash
cairo                                    # interactive chat
cairo "explain recursion"                # one-shot query
cairo -session myproject                 # named session (persisted)
cairo -provider openai "hello"           # switch provider
cairo -provider gemini -model gemini-2.0-flash "hi"

# Session management
cairo sessions list
cairo sessions rename default work
cairo sessions delete old-session

# Skills
cairo skills list
cairo -skills-dir ./my-skills skills list

# Telegram bot
cairo telegram
```

## Providers & Models

| Provider    | Default model              | Env var               |
|-------------|----------------------------|-----------------------|
| `gemini`    | gemini-2.0-flash           | `GEMINI_API_KEY`      |
| `anthropic` | claude-sonnet-4-20250514   | `ANTHROPIC_API_KEY`   |
| `openai`    | gpt-4o                     | `OPENAI_API_KEY`      |

## Skills

Drop a `.yaml` file in `.cairo/skills/` and it becomes a tool on the next run — no recompile needed.

**Prompt-only** (wraps an LLM call):
```yaml
name: summarize
description: Summarize any text concisely in bullet points
prompt: |
  Summarize the following in 3 bullet points:
  {{.Input}}
```

**Command-only** (wraps a shell command):
```yaml
name: count_lines
description: Count lines in a file
command: "wc -l {{.Input}}"
```

**Pipeline** (shell → LLM):
```yaml
name: git_log
description: Summarize recent git commits
command: "git -C {{.Input}} log --oneline -20"
prompt: |
  Commits:
  {{.Output}}
  Briefly summarize what changed.
```

Template variables: `{{.Input}}` is what the agent passes in, `{{.Output}}` is the command's stdout.

## File Structure

```
cairo/
├── main.go               # entry point, subcommands, flag parsing
├── .env                  # your API keys (never committed)
├── .cairo/               # auto-created on first run
│   ├── cairo.json        # sessions + message history
│   ├── config.yaml       # optional — created by: cairo init
│   └── skills/           # add .yaml skill files here
├── agent/
│   ├── react.go          # ReAct loop (Thought → Action → Observation)
│   └── session.go        # session loading, history, persistence
├── chat/
│   ├── cli.go            # interactive terminal with streaming
│   └── telegram.go       # Telegram bot via long-polling
├── config/
│   └── config.go         # load .env + config.yaml + env vars
├── db/
│   └── db.go             # JSON store — no SQLite, no CGo
├── llm/
│   ├── provider.go       # Provider interface (Chat + Stream)
│   ├── anthropic.go      # Anthropic — raw net/http + SSE streaming
│   ├── gemini.go         # Gemini — raw net/http + SSE streaming
│   ├── openai.go         # OpenAI — raw net/http + SSE streaming
│   └── factory.go        # build provider from config + flags
├── skills/
│   ├── skill.go          # YAML loader + SkillTool
│   └── examples/         # sample skill files
└── tools/
    ├── registry.go       # Tool interface + registry
    ├── shell.go          # run shell commands
    ├── file.go           # read_file, write_file, list_dir
    └── fetch.go          # HTTP GET with HTML stripping
```

## CLI Commands

| Command | Description |
|---|---|
| `/help` | show available commands |
| `/history` | print messages in this session |
| `/clear` | wipe session history |
| `/info` | show session metadata |
| `/exit` | quit |

## Build

```bash
make build          # current platform
make linux          # Linux amd64
make darwin         # macOS arm64 (Apple Silicon)
make darwin-amd     # macOS amd64 (Intel)
make windows        # Windows amd64
make all            # all platforms
make install        # go install to GOPATH/bin
```

Binaries are stamped with the version from `git describe --tags`.

## Gitignore

```
.env
.cairo/
```