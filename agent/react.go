package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/themillenniumfalcon/cairo/llm"
	"github.com/themillenniumfalcon/cairo/tools"
)

const maxIterations = 10

// reactSystemPrompt is appended to the base system prompt when tools are available.
const reactSystemPrompt = `
You can use tools to help answer questions. Use this exact format when you need a tool:

Thought: <your reasoning about what to do>
Action: <tool_name>
Action Input: <input for the tool>

After receiving an observation, continue reasoning. When you have the final answer:

Thought: I now know the answer.
Final Answer: <your response to the user>

Rules:
- Always start with a Thought.
- Use one Action at a time.
- If you don't need any tools, go straight to Final Answer.
- Never make up tool outputs. Wait for the Observation.
`

// Step is one iteration of the ReAct loop (for display purposes).
type Step struct {
	Thought     string
	Action      string
	ActionInput string
	Observation string
}

// RunReAct runs the ReAct loop for a user message and returns the final answer.
// It calls onStep(step) after each tool use so the caller can print progress.
func RunReAct(
	ctx context.Context,
	provider llm.Provider,
	registry *tools.Registry,
	history []llm.Message,
	onStep func(Step),
) (string, error) {

	// Build the working message list for this loop.
	// We don't modify the caller's history — we extend a local copy.
	msgs := make([]llm.Message, len(history))
	copy(msgs, history)

	for i := 0; i < maxIterations; i++ {
		reply, err := provider.Chat(ctx, msgs)
		if err != nil {
			return "", err
		}

		// Append the raw assistant reply to local history
		msgs = append(msgs, llm.Message{Role: llm.RoleAssistant, Content: reply})

		// Check for Final Answer first
		if fa := ExtractFinalAnswer(reply); fa != "" {
			return fa, nil
		}

		// Parse Thought / Action / Action Input
		thought, action, actionInput, ok := ParseReActReply(reply)
		if !ok {
			// LLM gave a plain response with no ReAct structure — treat as final answer
			return reply, nil
		}

		// Run the tool
		observation := registry.Run(action, actionInput)

		step := Step{
			Thought:     thought,
			Action:      action,
			ActionInput: actionInput,
			Observation: observation,
		}
		if onStep != nil {
			onStep(step)
		}

		// Feed the observation back as a user message (standard ReAct pattern)
		obsMsg := fmt.Sprintf("Observation: %s", observation)
		msgs = append(msgs, llm.Message{Role: llm.RoleUser, Content: obsMsg})
	}

	return "", fmt.Errorf("agent: reached max iterations (%d) without a final answer", maxIterations)
}

// BuildSystemPrompt returns the system prompt with the tool block injected.
func BuildSystemPrompt(registry *tools.Registry) string {
	base := systemPrompt // from session.go
	toolBlock := registry.PromptBlock()
	if toolBlock == "" {
		return base
	}
	return base + "\n\n" + toolBlock + reactSystemPrompt
}

// ── parsers ───────────────────────────────────────────────────────────────────

func ExtractFinalAnswer(s string) string {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if after, ok := cutPrefix(line, "Final Answer:"); ok {
			return strings.TrimSpace(after)
		}
	}
	return ""
}

// parseReActReply extracts Thought/Action/Action Input from an LLM reply.
// Returns ok=false if the reply doesn't follow the ReAct format.
func ParseReActReply(s string) (thought, action, actionInput string, ok bool) {
	lines := strings.Split(s, "\n")

	var (
		inActionInput bool
		aiLines       []string
	)

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if inActionInput {
			// Keep accumulating until we hit a new keyword
			if isKeyword(trimmed) {
				inActionInput = false
			} else {
				aiLines = append(aiLines, line)
				continue
			}
		}

		if v, ok2 := cutPrefix(trimmed, "Thought:"); ok2 {
			thought = strings.TrimSpace(v)
		} else if v, ok2 := cutPrefix(trimmed, "Action:"); ok2 {
			action = strings.TrimSpace(v)
		} else if v, ok2 := cutPrefix(trimmed, "Action Input:"); ok2 {
			actionInput = strings.TrimSpace(v)
			inActionInput = true
		}
	}

	// Collect any multiline Action Input
	if len(aiLines) > 0 {
		extra := strings.TrimSpace(strings.Join(aiLines, "\n"))
		if extra != "" {
			if actionInput != "" {
				actionInput = actionInput + "\n" + extra
			} else {
				actionInput = extra
			}
		}
	}

	ok = action != ""
	return
}

func isKeyword(s string) bool {
	return strings.HasPrefix(s, "Thought:") ||
		strings.HasPrefix(s, "Action:") ||
		strings.HasPrefix(s, "Observation:") ||
		strings.HasPrefix(s, "Final Answer:")
}

// cutPrefix is strings.CutPrefix (available since Go 1.20).
// Reimplemented here for safety with Go 1.22 (already available, but explicit).
func cutPrefix(s, prefix string) (string, bool) {
	if strings.HasPrefix(s, prefix) {
		return s[len(prefix):], true
	}
	return "", false
}
