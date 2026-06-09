package rag

import (
	"context"
	"os"
	"strings"
)

// Knowledge gaps — turn "ask your docs" usage into a content roadmap. Every ask
// is logged with its retrieval confidence; the questions the corpus repeatedly
// failed to answer are exactly the docs worth writing next. This is the loop that
// makes the wiki improve itself.

// logAsksEnabled reports whether ask logging is on (default yes; set
// TELA_RAG_LOG_ASKS=0 to disable for privacy-conscious instances).
func logAsksEnabled() bool {
	return os.Getenv("TELA_RAG_LOG_ASKS") != "0"
}

// LogAsk records one ask. Best-effort: a logging failure must never break the
// ask, so callers ignore the returned error (it's there for tests). No-op when
// disabled or the question is blank.
func (s *Service) LogAsk(ctx context.Context, userID int64, spaceID *int64, question string, hitCount int, topScore float64) error {
	if !logAsksEnabled() {
		return nil
	}
	question = strings.TrimSpace(question)
	if question == "" {
		return nil
	}
	answered := 0
	if hitCount > 0 {
		answered = 1
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO ask_log (user_id, space_id, question, hit_count, top_score, answered)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		userID, spaceID, question, hitCount, topScore, answered)
	return err
}

// KnowledgeGap is one repeatedly-unanswered question, aggregated.
type KnowledgeGap struct {
	Question  string  `json:"question"`   // the question text (most recent phrasing of the group)
	Asks      int     `json:"asks"`       // times this (normalized) question was asked
	Answered  int     `json:"answered"`   // of those, how many retrieved anything
	AvgHits   float64 `json:"avg_hits"`   // mean chunks retrieved across asks
	LastAsked string  `json:"last_asked"` // most recent ask timestamp
}

// KnowledgeGaps returns the most-asked questions that retrieval kept failing to
// answer — grouped by normalized question, filtered to those answered less than
// half the time, ranked by frequency then recency. sinceDays bounds the window
// (≤0 ⇒ all time). This is admin-facing analytics (it exposes users' questions);
// the API layer gates it.
func (s *Service) KnowledgeGaps(ctx context.Context, sinceDays, limit int) ([]KnowledgeGap, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	qb := &queryBuilder{}
	where := `question <> ''`
	if sinceDays > 0 {
		// created_at is TEXT 'YYYY-MM-DD HH:MM:SS' UTC; compare against a computed bound.
		where += ` AND created_at >= to_char((now() AT TIME ZONE 'UTC') - ` + qb.arg(sinceDays) + ` * interval '1 day', 'YYYY-MM-DD HH24:MI:SS')`
	}
	q := `
		SELECT (array_agg(question ORDER BY created_at DESC))[1] AS question,
		       count(*)                AS asks,
		       sum(answered)           AS answered,
		       avg(hit_count)::float8  AS avg_hits,
		       max(created_at)         AS last_asked
		  FROM ask_log
		 WHERE ` + where + `
		 GROUP BY lower(btrim(question))
		HAVING sum(answered) * 2 < count(*)
		 ORDER BY count(*) DESC, max(created_at) DESC
		 LIMIT ` + qb.arg(limit)
	rows, err := s.db.QueryContext(ctx, q, qb.args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []KnowledgeGap{}
	for rows.Next() {
		var g KnowledgeGap
		if err := rows.Scan(&g.Question, &g.Asks, &g.Answered, &g.AvgHits, &g.LastAsked); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}
