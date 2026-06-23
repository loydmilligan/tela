package mailer

import (
	"bytes"
	"fmt"
	"html/template"
	"net/url"
	"strings"
)

// Branded transactional templates. Email clients don't support OKLCH or CSS
// custom properties, so these inline hex values translate tela's "Loom in the
// dark" palette (landing/src/styles/tokens.css) into email-safe colors:
//
//	void #14121b · card #1d1b27 · hairline #322f3d
//	text #f3f1f8 / #b6b2c4 · indigo fill #4f46e5 on #ffffff
//
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
	clrLink   = "#8e88f0"
)

// monogramColors is a small fixed accent palette for actor monogram chips —
// deterministic per name (no avatar storage; renders without an image fetch, so
// it survives image-blocking clients). Hand-picked to read on white text.
var monogramColors = []string{
	"#4f46e5", "#0891b2", "#7c3aed", "#c026d3",
	"#db2777", "#e11d48", "#ca8a04", "#15803d",
}

// NotifLink is one suggested page in the "Related in this wiki" block.
type NotifLink struct {
	Label string
	URL   string
}

// NotifEmail is the content of a notification email. The heading reads as a
// sentence — actor (bold) + action + target (bold), e.g. "Ada mentioned you in
// Roadmap" — so who-did-what-where is obvious at a glance. Snippet and Related
// are optional per event type.
type NotifEmail struct {
	To        string
	Subject   string
	Eyebrow   string // small uppercase label, e.g. "Mention"
	Actor     string // display name; drives the monogram chip + bold lead
	Action    string // "mentioned you in"
	Target    string // "Page Title" (bold); may be empty
	Snippet   string // optional quoted excerpt (mention text / reply body)
	CTALabel  string
	CTAURL    string
	Related   []NotifLink // optional suggestions
	Footer    string      // why-you-got-this line
	ManageURL string      // link to /settings?tab=notifications
}

// emailView is the rendering model for the shared layout. Either Heading (a
// plain headline, used by verify/reset/feedback) OR Actor/Action/Target (the
// notification sentence) is set, never both.
type emailView struct {
	LogoOrigin string
	Eyebrow    string
	Heading    string
	Actor      string
	Action     string
	Target     string
	Intro      string
	Snippet    string
	Mono       string // monogram initial; "" hides the chip
	MonoColor  string
	CTALabel   string
	CTAURL     string
	Related    []NotifLink
	ShowPaste  bool // show the copy-paste URL fallback (verify/reset want it)
	Footer     string
	ManageURL  string
}

// Color helpers exposed to the template.
func (v emailView) C(name string) string {
	switch name {
	case "void":
		return clrVoid
	case "card":
		return clrCard
	case "rule":
		return clrRule
	case "text":
		return clrText
	case "muted":
		return clrMuted
	case "faint":
		return clrFaint
	case "indigo":
		return clrIndigo
	case "link":
		return clrLink
	}
	return clrText
}

// emailTmpl is parsed once. html/template gives contextual escaping for every
// interpolated field (actor/title/snippet are user-controlled), so there's no
// hand-rolled EscapeString to forget.
var emailTmpl = template.Must(template.New("email").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta name="color-scheme" content="dark"></head>
<body style="margin:0;padding:0;background:{{.C "void"}};">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="background:{{.C "void"}};padding:32px 16px;">
<tr><td align="center">
  <table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="max-width:480px;background:{{.C "card"}};border:1px solid {{.C "rule"}};border-radius:12px;overflow:hidden;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;">
    <tr><td style="padding:28px 32px 0 32px;">
      {{if .LogoOrigin}}<img src="{{.LogoOrigin}}/icon-64.png" width="26" height="26" alt="" style="vertical-align:middle;border-radius:7px;display:inline-block;"><span style="font-size:18px;font-weight:700;letter-spacing:-0.01em;color:{{.C "text"}};vertical-align:middle;margin-left:9px;">tela</span>{{else}}<span style="font-size:18px;font-weight:700;letter-spacing:-0.01em;color:{{.C "text"}};">tela</span>{{end}}
    </td></tr>
    {{if .Eyebrow}}<tr><td style="padding:22px 32px 0 32px;">
      <span style="font-size:11px;font-weight:700;letter-spacing:0.08em;text-transform:uppercase;color:{{.C "faint"}};">{{.Eyebrow}}</span>
    </td></tr>{{end}}
    {{if .Mono}}<tr><td style="padding:14px 32px 0 32px;">
      <span style="display:inline-block;width:34px;height:34px;border-radius:50%;background:{{.MonoColor}};color:#ffffff;font-size:15px;font-weight:700;line-height:34px;text-align:center;">{{.Mono}}</span>
    </td></tr>{{end}}
    <tr><td style="padding:{{if .Mono}}10{{else}}18{{end}}px 32px 0 32px;">
      <h1 style="margin:0;font-size:21px;line-height:1.35;font-weight:650;color:{{.C "text"}};">{{if .Heading}}{{.Heading}}{{else}}{{if .Actor}}<strong style="font-weight:750;">{{.Actor}}</strong> {{end}}{{.Action}}{{if .Target}} <strong style="font-weight:750;">{{.Target}}</strong>{{end}}{{end}}</h1>
    </td></tr>
    {{if .Intro}}<tr><td style="padding:12px 32px 0 32px;">
      <p style="margin:0;font-size:15px;line-height:1.6;color:{{.C "muted"}};">{{.Intro}}</p>
    </td></tr>{{end}}
    {{if .Snippet}}<tr><td style="padding:18px 32px 0 32px;">
      <table role="presentation" width="100%" cellpadding="0" cellspacing="0"><tr>
        <td style="border-left:3px solid {{.C "indigo"}};background:{{.C "void"}};border-radius:0 8px 8px 0;padding:12px 16px;">
          <p style="margin:0;font-size:14px;line-height:1.6;color:{{.C "muted"}};font-style:italic;">{{.Snippet}}</p>
        </td>
      </tr></table>
    </td></tr>{{end}}
    <tr><td style="padding:24px 32px 4px 32px;">
      <a href="{{.CTAURL}}" style="display:inline-block;background:{{.C "indigo"}};color:#ffffff;text-decoration:none;font-size:15px;font-weight:600;padding:12px 22px;border-radius:8px;">{{.CTALabel}}</a>
    </td></tr>
    {{if .ShowPaste}}<tr><td style="padding:18px 32px 0 32px;">
      <p style="margin:0;font-size:13px;line-height:1.5;color:{{.C "faint"}};">Or paste this link into your browser:<br>
        <a href="{{.CTAURL}}" style="color:{{.C "link"}};word-break:break-all;">{{.CTAURL}}</a></p>
    </td></tr>{{end}}
    {{if .Related}}<tr><td style="padding:26px 32px 0 32px;">
      <p style="margin:0 0 10px 0;font-size:12px;font-weight:700;letter-spacing:0.06em;text-transform:uppercase;color:{{.C "faint"}};">Related in this wiki</p>
      {{range .Related}}<a href="{{.URL}}" style="display:block;font-size:14px;line-height:1.5;color:{{$.C "link"}};text-decoration:none;padding:4px 0;">{{.Label}}</a>{{end}}
    </td></tr>{{end}}
    <tr><td style="padding:24px 32px 28px 32px;border-top:1px solid {{.C "rule"}};">
      <p style="margin:16px 0 0 0;font-size:12px;line-height:1.5;color:{{.C "faint"}};">{{.Footer}}{{if .ManageURL}} <a href="{{.ManageURL}}" style="color:{{.C "link"}};">Manage notification emails</a>.{{end}}</p>
    </td></tr>
  </table>
</td></tr>
</table>
</body></html>`))

// renderHTML executes the shared template into a string.
func renderHTML(v emailView) string {
	var b bytes.Buffer
	if err := emailTmpl.Execute(&b, v); err != nil {
		// Template is static + fields are strings; an error here is a programming
		// bug, not runtime data. Degrade to plaintext rather than send nothing.
		return v.Intro
	}
	return b.String()
}

// renderText is the plaintext alternative — same content, no markup.
func renderText(v emailView) string {
	var b strings.Builder
	b.WriteString("tela\n\n")
	if v.Eyebrow != "" {
		b.WriteString(strings.ToUpper(v.Eyebrow) + "\n")
	}
	switch {
	case v.Heading != "":
		b.WriteString(v.Heading)
	default:
		if v.Actor != "" {
			b.WriteString(v.Actor + " ")
		}
		b.WriteString(v.Action)
		if v.Target != "" {
			b.WriteString(" " + v.Target)
		}
	}
	b.WriteString("\n")
	if v.Intro != "" {
		b.WriteString("\n" + v.Intro + "\n")
	}
	if v.Snippet != "" {
		b.WriteString("\n  " + v.Snippet + "\n")
	}
	b.WriteString("\n" + v.CTALabel + ":\n" + v.CTAURL + "\n")
	if len(v.Related) > 0 {
		b.WriteString("\nRelated in this wiki:\n")
		for _, r := range v.Related {
			b.WriteString("  - " + r.Label + " — " + r.URL + "\n")
		}
	}
	b.WriteString("\n" + v.Footer)
	if v.ManageURL != "" {
		b.WriteString(" Manage notification emails: " + v.ManageURL)
	}
	b.WriteString("\n")
	return b.String()
}

// monogram returns the uppercase leading rune of name + a deterministic accent
// color. Empty name → no chip.
func monogram(name string) (initial, color string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", ""
	}
	r := []rune(name)
	initial = strings.ToUpper(string(r[0]))
	var sum int
	for _, c := range name {
		sum += int(c)
	}
	return initial, monogramColors[sum%len(monogramColors)]
}

// VerifyEmail builds the "confirm your email" message addressed to `to`.
// verifyURL is the full link carrying the raw token.
func VerifyEmail(to, username, verifyURL string) Message {
	intro := fmt.Sprintf("Welcome to tela, %s. Confirm this address to activate your account and start writing.", username)
	v := emailView{
		LogoOrigin: originOf(verifyURL),
		Heading:    "Confirm your email",
		Intro:      intro,
		CTALabel:   "Confirm email",
		CTAURL:     verifyURL,
		ShowPaste:  true,
		Footer:     "This link expires in 24 hours. If you didn't create a tela account, you can ignore this email.",
	}
	return Message{To: to, Subject: "Confirm your tela account", HTML: renderHTML(v), Text: renderText(v)}
}

// ResetPassword builds the "reset your password" message addressed to `to`.
// resetURL carries the raw token.
func ResetPassword(to, username, resetURL string) Message {
	intro := fmt.Sprintf("We received a request to reset the password for your tela account, %s. Choose a new one below.", username)
	v := emailView{
		LogoOrigin: originOf(resetURL),
		Heading:    "Reset your password",
		Intro:      intro,
		CTALabel:   "Reset password",
		CTAURL:     resetURL,
		ShowPaste:  true,
		Footer:     "This link expires in 1 hour. If you didn't request this, your password is unchanged and you can ignore this email.",
	}
	return Message{To: to, Subject: "Reset your tela password", HTML: renderHTML(v), Text: renderText(v)}
}

// FeedbackNotice tells an instance admin that new feedback landed. `who` is the
// submitter label, `subject`/`body` the feedback, and inboxURL the deep link to
// the admin Feedback tab. Body is truncated to keep the email sane.
func FeedbackNotice(to, who, subject, body, inboxURL string) Message {
	b := strings.TrimSpace(body)
	if len(b) > 600 {
		b = b[:600] + "…"
	}
	v := emailView{
		LogoOrigin: originOf(inboxURL),
		Eyebrow:    "Feedback",
		Heading:    "New feedback",
		Intro:      fmt.Sprintf("%s submitted feedback — “%s”: %s", who, subject, b),
		CTALabel:   "Open feedback inbox",
		CTAURL:     inboxURL,
		Footer:     "You're receiving this because you're a tela instance admin.",
	}
	return Message{To: to, Subject: "New tela feedback: " + subject, HTML: renderHTML(v), Text: renderText(v)}
}

// NotificationMessage builds a notification email from n. The actor anchors the
// email — a monogram chip + a bold lead in the heading sentence.
func NotificationMessage(n NotifEmail) Message {
	mono, color := monogram(n.Actor)
	v := emailView{
		LogoOrigin: originOf(n.CTAURL),
		Eyebrow:    n.Eyebrow,
		Actor:      n.Actor,
		Action:     n.Action,
		Target:     n.Target,
		Snippet:    n.Snippet,
		Mono:       mono,
		MonoColor:  color,
		CTALabel:   n.CTALabel,
		CTAURL:     n.CTAURL,
		Related:    n.Related,
		Footer:     n.Footer,
		ManageURL:  n.ManageURL,
	}
	return Message{To: n.To, Subject: n.Subject, HTML: renderHTML(v), Text: renderText(v)}
}

// originOf returns scheme://host for a full URL, or "" when it can't be parsed.
// Used to point the email logo at the deploying instance's own /icon-64.png.
func originOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}
