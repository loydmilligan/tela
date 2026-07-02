package api

import (
	"context"

	"github.com/zcag/tela/backend/internal/mailer"
)

// meteredMailer wraps a mailer.Mailer so every Send bumps the tela_email_send_total
// counter (result=ok|error). It's the observability seam for email, which is
// otherwise a silent-failure path — see the emailSends metric in metrics.go. The
// wrapped error is returned unchanged; callers keep their existing behaviour.
type meteredMailer struct{ inner mailer.Mailer }

func (m meteredMailer) Send(ctx context.Context, msg mailer.Message) error {
	err := m.inner.Send(ctx, msg)
	result := "ok"
	if err != nil {
		result = "error"
	}
	emailSends.WithLabelValues(result).Inc()
	return err
}
