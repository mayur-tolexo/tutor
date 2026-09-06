package tutor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// LLM is the chat-completion dependency; implementations must be safe for
// concurrent use.
type LLM interface {
	Complete(ctx context.Context, req ChatRequest) (ChatResponse, error)
}

// ChatRequest is one system+user turn.
type ChatRequest struct {
	System      string
	User        string
	MaxTokens   int
	Temperature float64
}

// ChatResponse is the model's reply plus accounting.
type ChatResponse struct {
	Text   string
	Model  string
	Tokens int
}

// OpenAICompat talks to any OpenAI-compatible /chat/completions endpoint,
// which is how the NeevCloud inference service is exposed.
type OpenAICompat struct {
	BaseURL string // e.g. https://inference.example/v1
	APIKey  string
	Model   string
	// DisableThinking asks reasoning models to answer directly. Without it a
	// model like glm-5-2 spends the whole token budget on hidden reasoning and
	// returns empty content.
	DisableThinking bool
	HTTP            *http.Client
}

// NewOpenAICompat returns a client with a request timeout suited to short hints.
func NewOpenAICompat(baseURL, apiKey, model string) *OpenAICompat {
	return &OpenAICompat{
		BaseURL:         strings.TrimRight(baseURL, "/"),
		APIKey:          apiKey,
		Model:           model,
		DisableThinking: true,
		HTTP:            &http.Client{Timeout: 30 * time.Second},
	}
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatCompletionRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
	Temperature float64       `json:"temperature"`
	// Both spellings of "no thinking" are sent because servers differ: the
	// Z.ai/GLM style and the vLLM chat-template style.
	Thinking           *thinkingOpt   `json:"thinking,omitempty"`
	ChatTemplateKwargs map[string]any `json:"chat_template_kwargs,omitempty"`
}

type thinkingOpt struct {
	Type string `json:"type"`
}

type chatCompletionResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
	Usage struct {
		TotalTokens int `json:"total_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Complete sends one chat completion and returns the first choice's text.
func (c *OpenAICompat) Complete(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	ccr := chatCompletionRequest{
		Model:       c.Model,
		Messages:    []chatMessage{{Role: "system", Content: req.System}, {Role: "user", Content: req.User}},
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
	}
	if c.DisableThinking {
		ccr.Thinking = &thinkingOpt{Type: "disabled"}
		ccr.ChatTemplateKwargs = map[string]any{"enable_thinking": false}
	}
	body, err := json.Marshal(ccr)
	if err != nil {
		return ChatResponse{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return ChatResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	resp, err := c.HTTP.Do(httpReq)
	if err != nil {
		return ChatResponse{}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return ChatResponse{}, err
	}
	var out chatCompletionResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return ChatResponse{}, fmt.Errorf("llm: status %d: %s", resp.StatusCode, truncate(string(raw), 200))
	}
	if resp.StatusCode >= 300 || out.Error != nil {
		msg := ""
		if out.Error != nil {
			msg = out.Error.Message
		}
		return ChatResponse{}, fmt.Errorf("llm: status %d: %s", resp.StatusCode, msg)
	}
	if len(out.Choices) == 0 {
		return ChatResponse{}, fmt.Errorf("llm: no choices")
	}
	model := out.Model
	if model == "" {
		model = c.Model
	}
	return ChatResponse{Text: strings.TrimSpace(out.Choices[0].Message.Content), Model: model, Tokens: out.Usage.TotalTokens}, nil
}

// truncate shortens s for error messages.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
