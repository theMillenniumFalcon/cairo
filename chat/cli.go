package chat

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/themillenniumfalcon/cairo/llm"
)

const systemPrompt = `You are Cairo, a personal AI agent. You are helpful, concise, and direct.`

// CLI runs an interactive chat session in the terminal.
func CLI(provider llm.Provider) error {
	reader := bufio.NewReader(os.Stdin)

	fmt.Printf("Cairo  ·  %s / %s\n", provider.Name(), provider.Model())
	fmt.Println("Type your message. Ctrl+C or Ctrl+D to exit.")
	fmt.Println(strings.Repeat("─", 50))

	history := []llm.Message{
		{Role: llm.RoleSystem, Content: systemPrompt},
	}

	for {
		fmt.Print("you → ")

		line, err := reader.ReadString('\n')
		if err != nil {
			// EOF (Ctrl+D)
			fmt.Println("\nBye.")
			return nil
		}

		input := strings.TrimSpace(line)
		if input == "" {
			continue
		}
		if input == "exit" || input == "quit" {
			fmt.Println("Bye.")
			return nil
		}

		history = append(history, llm.Message{Role: llm.RoleUser, Content: input})

		fmt.Print("cairo → ")
		reply, err := provider.Chat(context.Background(), history)
		if err != nil {
			fmt.Printf("error: %v\n\n", err)
			history = history[:len(history)-1]
			continue
		}

		fmt.Println(reply)
		fmt.Println()

		history = append(history, llm.Message{Role: llm.RoleAssistant, Content: reply})
	}
}
