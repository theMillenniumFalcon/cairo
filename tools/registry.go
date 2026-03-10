package tools

import "fmt"

// Tool is something the agent can invoke by name.
type Tool interface {
	Name() string
	Description() string // one-line description shown to the LLM
	Run(input string) (string, error)
}

// Registry holds all registered tools.
type Registry struct {
	tools map[string]Tool
	order []string // preserve registration order for prompt building
}

func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]Tool)}
}

// Register adds a tool. Panics on duplicate name.
func (r *Registry) Register(t Tool) {
	if _, exists := r.tools[t.Name()]; exists {
		panic("tool already registered: " + t.Name())
	}
	r.tools[t.Name()] = t
	r.order = append(r.order, t.Name())
}

// Get returns a tool by name.
func (r *Registry) Get(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

// All returns tools in registration order.
func (r *Registry) All() []Tool {
	out := make([]Tool, 0, len(r.order))
	for _, name := range r.order {
		out = append(out, r.tools[name])
	}
	return out
}

// Run executes a named tool and returns its output or an error string.
func (r *Registry) Run(name, input string) string {
	t, ok := r.tools[name]
	if !ok {
		return fmt.Sprintf("error: unknown tool %q", name)
	}
	out, err := t.Run(input)
	if err != nil {
		return fmt.Sprintf("error: %v", err)
	}
	return out
}

// PromptBlock returns the tool-list block injected into the system prompt.
func (r *Registry) PromptBlock() string {
	if len(r.tools) == 0 {
		return ""
	}
	s := "You have access to the following tools:\n\n"
	for _, t := range r.All() {
		s += fmt.Sprintf("- %s: %s\n", t.Name(), t.Description())
	}
	return s
}
