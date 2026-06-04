package mailer

import (
	"fmt"
	"html"
	"strings"
)

// Branded transactional templates. Email clients don't support OKLCH or CSS
// custom properties, so these inline hex values translate tela's "Loom in the
// dark" palette (landing/src/styles/tokens.css) into email-safe colors:
//   void #14121b · card #1d1b27 · hairline #322f3d
//   text #f3f1f8 / #b6b2c4 · indigo fill #4f46e5 on #ffffff
// This is the one place hex is correct — the frontend token gate does not
// (and cannot) cover server-rendered email.
const (
	clrVoid   = "#14121b"
	clrCard   = "#1d1b27"
	clrRule   = "#322f3d"
	clrText   = "#f3f1f8"
	clrMuted  = "#b6b2c4"
	clrFaint  = "#8a8699"
	clrIndigo = "#4f46e5"
)

// VerifyEmail builds the "confirm your email" message. verifyURL is the full
// link carrying the raw token.
func VerifyEmail(username, verifyURL string) Message {
	intro := fmt.Sprintf("Welcome to tela, %s. Confirm this address to activate your account and start writing.", username)
	return Message{
		Subject: "Confirm your tela account",
		HTML:    layoutHTML("Confirm your email", intro, "Confirm email", verifyURL, "This link expires in 24 hours. If you didn't create a tela account, you can ignore this email."),
		Text:    layoutText(intro, "Confirm email", verifyURL, "This link expires in 24 hours. If you didn't create a tela account, you can ignore this email."),
	}
}

// ResetPassword builds the "reset your password" message. resetURL carries the
// raw token.
func ResetPassword(username, resetURL string) Message {
	intro := fmt.Sprintf("We received a request to reset the password for your tela account, %s. Choose a new one below.", username)
	return Message{
		Subject: "Reset your tela password",
		HTML:    layoutHTML("Reset your password", intro, "Reset password", resetURL, "This link expires in 1 hour. If you didn't request this, your password is unchanged and you can ignore this email."),
		Text:    layoutText(intro, "Reset password", resetURL, "This link expires in 1 hour. If you didn't request this, your password is unchanged and you can ignore this email."),
	}
}

// layoutHTML renders the shared dark, indigo-accented card used by every
// transactional email: tela wordmark, heading, one paragraph, a single CTA
// button, the raw URL as a copy-paste fallback, and a footer note.
func layoutHTML(heading, intro, ctaLabel, ctaURL, footer string) string {
	h := html.EscapeString
	return `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"></head>
<body style="margin:0;padding:0;background:` + clrVoid + `;">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="background:` + clrVoid + `;padding:32px 16px;">
<tr><td align="center">
  <table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="max-width:480px;background:` + clrCard + `;border:1px solid ` + clrRule + `;border-radius:12px;overflow:hidden;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;">
    <tr><td style="padding:28px 32px 0 32px;">
      <span style="font-size:18px;font-weight:700;letter-spacing:-0.01em;color:` + clrText + `;">tela</span>
    </td></tr>
    <tr><td style="padding:20px 32px 0 32px;">
      <h1 style="margin:0;font-size:22px;line-height:1.3;font-weight:650;color:` + clrText + `;">` + h(heading) + `</h1>
    </td></tr>
    <tr><td style="padding:12px 32px 0 32px;">
      <p style="margin:0;font-size:15px;line-height:1.6;color:` + clrMuted + `;">` + h(intro) + `</p>
    </td></tr>
    <tr><td style="padding:24px 32px 4px 32px;">
      <a href="` + h(ctaURL) + `" style="display:inline-block;background:` + clrIndigo + `;color:#ffffff;text-decoration:none;font-size:15px;font-weight:600;padding:12px 22px;border-radius:8px;">` + h(ctaLabel) + `</a>
    </td></tr>
    <tr><td style="padding:18px 32px 0 32px;">
      <p style="margin:0;font-size:13px;line-height:1.5;color:` + clrFaint + `;">Or paste this link into your browser:<br>
        <a href="` + h(ctaURL) + `" style="color:#8e88f0;word-break:break-all;">` + h(ctaURL) + `</a></p>
    </td></tr>
    <tr><td style="padding:24px 32px 28px 32px;border-top:1px solid ` + clrRule + `;margin-top:8px;">
      <p style="margin:16px 0 0 0;font-size:12px;line-height:1.5;color:` + clrFaint + `;">` + h(footer) + `</p>
    </td></tr>
  </table>
</td></tr>
</table>
</body></html>`
}

// layoutText is the plaintext alternative — same content, no markup.
func layoutText(intro, ctaLabel, ctaURL, footer string) string {
	var b strings.Builder
	b.WriteString("tela\n\n")
	b.WriteString(intro)
	b.WriteString("\n\n")
	b.WriteString(ctaLabel + ":\n")
	b.WriteString(ctaURL)
	b.WriteString("\n\n")
	b.WriteString(footer)
	b.WriteString("\n")
	return b.String()
}
