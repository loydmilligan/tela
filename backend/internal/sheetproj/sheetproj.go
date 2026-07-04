// Package sheetproj turns a sheet's Defter body into self-describing prose for
// LLM / embedder consumption: styling stripped, tables prose-ified, and
// formula-COMPUTED values materialized. The formula engine is TypeScript, so
// computed values come from the node sidecar (TELA_DECK_URL + /project); when
// the sidecar is unset or unreachable it falls back to the in-process Go
// projection (defterparse — literal values, formulas as source). Shared by the
// RAG indexer and the summarizer so a sheet reads the same way to both.
package sheetproj

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	defterparse "github.com/zcag/defter/go"
)

// Project returns embed/summary-ready prose for a sheet body.
func Project(ctx context.Context, body string) string {
	if base := strings.TrimRight(os.Getenv("TELA_DECK_URL"), "/"); base != "" {
		if prose, err := postProject(ctx, base+"/project", body); err == nil {
			return prose
		}
	}
	return defterparse.ProjectProse(defterparse.Parse(body))
}

func postProject(ctx context.Context, url, body string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("content-type", "text/plain; charset=utf-8")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
		return "", fmt.Errorf("project sidecar %d", resp.StatusCode)
	}
	out, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", err
	}
	if len(strings.TrimSpace(string(out))) == 0 {
		return "", fmt.Errorf("project sidecar returned empty")
	}
	return string(out), nil
}
