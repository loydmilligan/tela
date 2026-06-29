package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"
)

// ai_usage.go — record + read the per-call AI usage log (table ai_usage,
// migration 0044). Capture happens at the service chokepoints: the LLM service
// (chat), the RAG embedder (embed), and image generation (image) each call
// s.recordAIUsage via an injected recorder, so there's no per-call-site wiring.
// Token counts are length-based ESTIMATES (see EstimateTokens) — fine for the
// cost-estimation use case, not exact billing.

// recordAIUsage appends one usage row and increments the Prometheus token
// counter. Best-effort: detached context (some callers are background workers)
// and errors are logged, never propagated.
func (s *Server) recordAIUsage(kind, model string, inTokens, outTokens, units int) {
	aiTokens.WithLabelValues(kind).Add(float64(inTokens + outTokens))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := s.DB.ExecContext(ctx,
		`INSERT INTO ai_usage (kind, model, input_tokens, output_tokens, units)
		 VALUES ($1, $2, $3, $4, $5)`,
		kind, model, inTokens, outTokens, units); err != nil {
		slog.Error("ai_usage record failed", "kind", kind, "model", model, "err", err)
	}
}

type aiUsageWeek struct {
	Week        string `json:"week"` // Monday of the ISO week, 'YYYY-MM-DD'
	ChatTokens  int64  `json:"chat_tokens"`
	EmbedTokens int64  `json:"embed_tokens"`
	Images      int64  `json:"images"`
}

type aiUsageModel struct {
	Model  string `json:"model"`
	Kind   string `json:"kind"`
	Tokens int64  `json:"tokens"`
	Units  int64  `json:"units"`
	Calls  int64  `json:"calls"`
}

type aiUsageOut struct {
	Weeks  []aiUsageWeek  `json:"weeks"`
	Models []aiUsageModel `json:"models"`
}

// AdminAIUsage — GET /api/admin/ai-usage. Instance-admin only. Weekly token
// totals (chat/embed) + image counts for the last ~12 weeks, plus per-model
// totals over the window — the raw material for cost estimates.
func (s *Server) AdminAIUsage(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireInstanceAdmin(w, r); !ok {
		return
	}
	ctx := r.Context()
	cut := time.Now().UTC().AddDate(0, 0, -7*12).Format("2006-01-02 15:04:05")
	out := aiUsageOut{Weeks: []aiUsageWeek{}, Models: []aiUsageModel{}}

	if rows, err := s.DB.QueryContext(ctx, `
		SELECT to_char(date_trunc('week', substr(created_at,1,10)::date), 'YYYY-MM-DD') AS week,
		       COALESCE(SUM(input_tokens + output_tokens) FILTER (WHERE kind = 'chat'), 0),
		       COALESCE(SUM(input_tokens + output_tokens) FILTER (WHERE kind = 'embed'), 0),
		       COALESCE(SUM(units) FILTER (WHERE kind = 'image'), 0)
		  FROM ai_usage
		 WHERE created_at >= $1
		 GROUP BY week ORDER BY week`, cut); err == nil {
		defer rows.Close()
		for rows.Next() {
			var wk aiUsageWeek
			if err := rows.Scan(&wk.Week, &wk.ChatTokens, &wk.EmbedTokens, &wk.Images); err != nil {
				break
			}
			out.Weeks = append(out.Weeks, wk)
		}
		rows.Close()
	}

	if rows, err := s.DB.QueryContext(ctx, `
		SELECT model, kind,
		       SUM(input_tokens + output_tokens),
		       SUM(units),
		       COUNT(*)
		  FROM ai_usage
		 WHERE created_at >= $1
		 GROUP BY model, kind
		 ORDER BY SUM(input_tokens + output_tokens) DESC, COUNT(*) DESC
		 LIMIT 20`, cut); err == nil {
		defer rows.Close()
		for rows.Next() {
			var m aiUsageModel
			if err := rows.Scan(&m.Model, &m.Kind, &m.Tokens, &m.Units, &m.Calls); err != nil {
				break
			}
			out.Models = append(out.Models, m)
		}
		rows.Close()
	}

	writeJSON(w, http.StatusOK, out)
}
