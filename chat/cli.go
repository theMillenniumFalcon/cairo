package chat

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/themillenniumfalcon/cairo/agent"
	"github.com/themillenniumfalcon/cairo/llm"
)

// CLI runs an interactive chat session in the terminal.
// sess is a pre-loaded session (may have existing history).
func CLI(provider llm.Provider, sess *agent.Session) error {
	reader := bufio.NewReader(os.Stdin)

	fmt.Printf("Cairo  ·  %s / %s  ·  session: %s\n",
		provider.Name(), provider.Model(), sess.Record.Name)

	if sess.MessageCount() > 0 {
		fmt.Printf("Resuming — %d messages in history.\n", sess.MessageCount())
	}

	fmt.Println("Type your message. /help for commands. Ctrl+D to exit.")
	fmt.Println(strings.Repeat("─", 50))

	for {
		fmt.Print("you → ")

		line, err := reader.ReadString('\n')
		if err != nil {
			fmt.Println("\nBye.")
			return nil
		}

		input := strings.TrimSpace(line)
		if input == "" {
			continue
		}

		// Built-in slash commands
		if strings.HasPrefix(input, "/") {
			if handled, err := handleCommand(input, sess); handled {
				if err != nil {
					fmt.Printf("error: %v\n\n", err)
				}
				continue
			}
		}

		if input == "exit" || input == "quit" {
			fmt.Println("Bye.")
			return nil
		}

		if err := sess.Add(llm.RoleUser, input); err != nil {
			fmt.Printf("warning: could not save message: %v\n", err)
		}

		fmt.Print("cairo → ")
		reply, err := provider.Chat(context.Background(), sess.History)
		if err != nil {
			fmt.Printf("error: %v\n\n", err)
			// Roll back the user message from history on failure
			sess.History = sess.History[:len(sess.History)-1]
			continue
		}

		fmt.Println(reply)
		fmt.Println()

		if err := sess.Add(llm.RoleAssistant, reply); err != nil {
			fmt.Printf("warning: could not save reply: %v\n", err)
		}
	}
}

// handleCommand processes /commands. Returns (handled, err).
func handleCommand(input string, sess *agent.Session) (bool, error) {
	parts := strings.Fields(input)
	cmd := parts[0]

	switch cmd {
	case "/help":
		fmt.Println("Commands:")
		fmt.Println("  /history   show messages in this session")
		fmt.Println("  /clear     clear this session's history")
		fmt.Println("  /info      show session info")
		fmt.Println("  /exit      quit")
		return true, nil

	case "/history":
		msgs := sess.History
		count := 0
		for _, m := range msgs {
			if m.Role == "system" {
				continue
			}
			count++
			prefix := "you"
			if m.Role == "assistant" {
				prefix = "cairo"
			}
			content := m.Content
			if len(content) > 120 {
				content = content[:120] + "…"
			}
			fmt.Printf("[%d] %s: %s\n", count, prefix, content)
		}
		if count == 0 {
			fmt.Println("No messages yet.")
		}
		fmt.Println()
		return true, nil

	case "/clear":
		if err := sess.ClearHistory(); err != nil {
			return true, err
		}
		fmt.Println("History cleared.")
		return true, nil

	case "/info":
		fmt.Printf("Session : %s (id=%d)\n", sess.Record.Name, sess.Record.ID)
		fmt.Printf("Provider: %s / %s\n", sess.Record.Provider, sess.Record.Model)
		fmt.Printf("Messages: %d\n", sess.MessageCount())
		fmt.Printf("Created : %s\n\n", sess.Record.CreatedAt.Format("2006-01-02 15:04"))
		return true, nil

	case "/exit", "/quit":
		fmt.Println("Bye.")
		os.Exit(0)
	}

	return false, nil
}
