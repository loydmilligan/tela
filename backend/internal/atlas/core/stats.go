package core

// Usage is the token + call tally for one run, captured from the LLM responses.
type Usage struct {
	ChatCalls        int     `json:"chat_calls"`
	EmbedCalls       int     `json:"embed_calls"`
	PromptTokens     int64   `json:"prompt_tokens"`
	CompletionTokens int64   `json:"completion_tokens"`
	EmbedTokens      int64   `json:"embed_tokens"`
}

func (u Usage) ChatTokens() int64  { return u.PromptTokens + u.CompletionTokens }
func (u Usage) TotalTokens() int64 { return u.PromptTokens + u.CompletionTokens + u.EmbedTokens }

// RunStats is the per-run report shown on the doc top page + the UI run detail:
// what was processed, how long it took, what model, and the token usage + cost.
type RunStats struct {
	Files       int     `json:"files"`
	Surface     int     `json:"surface"`
	Chunks      int     `json:"chunks"`
	Pages       int     `json:"pages"`
	DurationSec float64 `json:"duration_sec"`
	ChatModel   string  `json:"chat_model"`
	EmbedModel  string  `json:"embed_model"`
	Usage       Usage   `json:"usage"`
	Cost        float64 `json:"cost"` // USD, 0 when the connection has no prices
}
