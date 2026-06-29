package mailer

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	mail "github.com/wneessen/go-mail"
)

// smtpConfig is the resolved relay configuration. from is the From header
// (and envelope sender); it may be a bare address or `Name <addr>` form.
type smtpConfig struct {
	host     string
	port     int
	username string
	password string
	from     string
	tls      string // starttls | ssl | none
}

// smtpConfigFromEnv reads the TELA_SMTP_* vars. The bool is false (→ LogMailer)
// when TELA_SMTP_HOST is unset, which is the only "not configured" signal.
func smtpConfigFromEnv() (smtpConfig, bool) {
	host := strings.TrimSpace(os.Getenv("TELA_SMTP_HOST"))
	if host == "" {
		return smtpConfig{}, false
	}
	port := 587
	if v := strings.TrimSpace(os.Getenv("TELA_SMTP_PORT")); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			port = n
		}
	}
	tls := strings.ToLower(strings.TrimSpace(os.Getenv("TELA_SMTP_TLS")))
	if tls == "" {
		tls = "starttls"
	}
	from := strings.TrimSpace(os.Getenv("TELA_SMTP_FROM"))
	if from == "" {
		from = os.Getenv("TELA_SMTP_USERNAME")
	}
	return smtpConfig{
		host:     host,
		port:     port,
		username: os.Getenv("TELA_SMTP_USERNAME"),
		password: os.Getenv("TELA_SMTP_PASSWORD"),
		from:     from,
		tls:      tls,
	}, true
}

// smtpMailer sends via an SMTP relay using go-mail. A fresh client + connection
// is dialed per Send — fine for tela's low transactional volume and simpler
// than pooling a long-lived connection that the relay may drop.
type smtpMailer struct {
	cfg smtpConfig
}

func (m *smtpMailer) Send(ctx context.Context, msg Message) error {
	mm := mail.NewMsg()
	if err := mm.From(m.cfg.from); err != nil {
		return fmt.Errorf("mailer: from %q: %w", m.cfg.from, err)
	}
	if err := mm.To(msg.To); err != nil {
		return fmt.Errorf("mailer: to %q: %w", msg.To, err)
	}
	mm.Subject(msg.Subject)
	if msg.Important {
		mm.SetImportance(mail.ImportanceHigh)
	}
	mm.SetBodyString(mail.TypeTextPlain, msg.Text)
	mm.AddAlternativeString(mail.TypeTextHTML, msg.HTML)

	opts := []mail.Option{mail.WithPort(m.cfg.port)}
	switch m.cfg.tls {
	case "ssl":
		opts = append(opts, mail.WithSSL())
	case "none":
		opts = append(opts, mail.WithTLSPolicy(mail.NoTLS))
	default: // starttls
		opts = append(opts, mail.WithTLSPolicy(mail.TLSMandatory))
	}
	if m.cfg.username != "" || m.cfg.password != "" {
		opts = append(opts,
			mail.WithSMTPAuth(mail.SMTPAuthPlain),
			mail.WithUsername(m.cfg.username),
			mail.WithPassword(m.cfg.password),
		)
	}

	c, err := mail.NewClient(m.cfg.host, opts...)
	if err != nil {
		return fmt.Errorf("mailer: new smtp client: %w", err)
	}
	if err := c.DialAndSendWithContext(ctx, mm); err != nil {
		return fmt.Errorf("mailer: send: %w", err)
	}
	return nil
}
