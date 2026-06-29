// Package mailer is tela's transactional email subsystem: a small Mailer
// interface plus two drivers. The SMTP driver (smtp.go) is provider-agnostic —
// it speaks to any SMTP relay (Resend, Postmark, SES, self-hosted Postfix),
// keeping tela self-hostable. When no SMTP host is configured the LogMailer
// fallback prints the message (and any action link) to the server log so the
// register/verify/reset flows work out-of-the-box in dev and on first boot.
package mailer

import (
	"context"
	"log/slog"
)

// Message is one transactional email. Text is the plaintext fallback; HTML is
// the rendered body. Both are sent as a multipart/alternative so every client
// renders something.
type Message struct {
	To      string
	Subject string
	HTML    string
	Text    string
	// Important marks the message high-importance (Importance/Priority/
	// X-Priority/X-MSMail-Priority headers) so it stands out in the inbox.
	// Used for feedback notices; leave false for routine transactional mail.
	Important bool
}

// Mailer sends transactional email. Implementations must be safe for
// concurrent use by multiple request handlers.
type Mailer interface {
	Send(ctx context.Context, msg Message) error
}

// FromEnv selects a driver from the environment. TELA_SMTP_HOST present →
// the SMTP driver; otherwise the LogMailer dev fallback. Never returns nil, so
// callers can depend on a non-nil Mailer without guarding.
//
//	TELA_SMTP_HOST       relay hostname (empty → LogMailer)
//	TELA_SMTP_PORT       default 587
//	TELA_SMTP_USERNAME   SMTP auth user (Resend: "resend")
//	TELA_SMTP_PASSWORD   SMTP auth pass (Resend: your API key)
//	TELA_SMTP_FROM       envelope/from, e.g. `tela <tela@example.com>`
//	TELA_SMTP_TLS        starttls (default) | ssl | none
func FromEnv() Mailer {
	cfg, ok := smtpConfigFromEnv()
	if !ok {
		slog.Warn("mailer: TELA_SMTP_HOST unset — using log fallback (emails printed, not sent)")
		return &LogMailer{}
	}
	slog.Info("mailer: SMTP relay", "host", cfg.host, "port", cfg.port, "tls", cfg.tls, "from", cfg.from)
	return &smtpMailer{cfg: cfg}
}

// LogMailer prints emails to the server log instead of sending them. Used when
// no SMTP relay is configured — the verify/reset links land in the log so a
// developer or first-time self-hoster can complete the flow without a relay.
type LogMailer struct{}

func (m *LogMailer) Send(_ context.Context, msg Message) error {
	slog.Info("mailer(log): email not sent (no relay)", "to", msg.To, "subject", msg.Subject, "text", msg.Text)
	return nil
}
