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
	clrVoid    = "#14121b"
	clrCard    = "#1d1b27"
	clrRule    = "#322f3d"
	clrText    = "#f3f1f8"
	clrMuted   = "#b6b2c4"
	clrFaint   = "#8a8699"
	clrIndigo  = "#4f46e5"
	clrIndigo2 = "#8b5cf6" // accent-bar gradient end
	clrLink    = "#8e88f0"
	clrQuote   = "#cfccd9"
	clrPanel   = "#191722" // footer / header panel, a hair darker than the card
	clrPill    = "#232030" // event-badge pill fill
)

// emailTagline rides the footer of every email — a one-line product signature.
const emailTagline = "tela — your team's markdown-native, AI-ready knowledge base."

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

// DiffLine is one line of a page_updated "what changed" preview — an addition
// (green +) or a deletion (red −).
type DiffLine struct {
	Add  bool
	Text string
}

// NotifEmail is the content of a notification email. The heading reads as a
// sentence — actor (bold) + action + target (bold), e.g. "Ada mentioned you in
// Roadmap" — so who-did-what-where is obvious at a glance. Snippet and Related
// are optional per event type.
type NotifEmail struct {
	To        string
	Subject   string
	Eyebrow   string     // small uppercase label, e.g. "Mention"
	Actor     string     // display name; drives the monogram chip + bold lead
	Action    string     // "mentioned you in"
	Target    string     // "Page Title" (bold); may be empty
	Context   string     // workspace breadcrumb, e.g. the space name; may be empty
	Snippet   string     // optional quoted excerpt (mention text / reply body)
	Diff      []DiffLine // optional "what changed" preview (page_updated)
	DiffStat  string     // e.g. "12 additions · 3 deletions"
	DiffMore  string     // e.g. "9 more changed lines"
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
	Context    string
	Intro      string
	Snippet    string
	Diff       []DiffLine
	DiffStat   string
	DiffMore   string
	Mono       string // monogram initial; "" hides the chip
	MonoColor  string
	CTALabel   string
	CTAURL     string
	Related    []NotifLink
	ShowPaste  bool // show the copy-paste URL fallback (verify/reset want it)
	Footer     string
	ManageURL  string
	Tagline    string // product signature in the footer; set by renderHTML
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
	case "quote":
		return clrQuote
	case "indigo2":
		return clrIndigo2
	case "panel":
		return clrPanel
	case "pill":
		return clrPill
	}
	return clrText
}

// emailTmpl is parsed once. html/template gives contextual escaping for every
// interpolated field (actor/title/snippet are user-controlled), so there's no
// hand-rolled EscapeString to forget.
var emailTmpl = template.Must(template.New("email").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta name="color-scheme" content="dark"></head>
<body style="margin:0;padding:0;background:{{.C "void"}};">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="background:{{.C "void"}};padding:40px 16px;">
<tr><td align="center">
  <table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="max-width:600px;background:{{.C "card"}};border:1px solid {{.C "rule"}};border-radius:16px;overflow:hidden;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;">
    <!-- accent bar -->
    <tr><td style="height:4px;line-height:4px;font-size:0;background:{{.C "indigo"}};background:linear-gradient(90deg,{{.C "indigo"}},{{.C "indigo2"}});">&nbsp;</td></tr>
    <!-- header -->
    <tr><td style="padding:22px 36px;border-bottom:1px solid {{.C "rule"}};">
      <table role="presentation" width="100%" cellpadding="0" cellspacing="0"><tr>
        <td style="vertical-align:middle;">
          {{if .LogoOrigin}}<img src="{{.LogoOrigin}}/icon-64.png" width="26" height="26" alt="" style="vertical-align:middle;border-radius:7px;display:inline-block;"><span style="font-size:18px;font-weight:700;letter-spacing:-0.01em;color:{{.C "text"}};vertical-align:middle;margin-left:9px;">tela</span>{{else}}<span style="font-size:18px;font-weight:700;letter-spacing:-0.01em;color:{{.C "text"}};">tela</span>{{end}}
        </td>
        {{if .Eyebrow}}<td align="right" style="vertical-align:middle;">
          <span style="display:inline-block;background:{{.C "pill"}};border:1px solid {{.C "rule"}};border-radius:999px;padding:5px 12px;font-size:11px;font-weight:700;letter-spacing:0.06em;text-transform:uppercase;color:{{.C "muted"}};">{{.Eyebrow}}</span>
        </td>{{end}}
      </tr></table>
    </td></tr>
    {{if .Actor}}
    <!-- notification body: who → what → which page -->
    <tr><td style="padding:30px 36px 0 36px;">
      {{if .Context}}<p style="margin:0 0 18px 0;font-size:12px;line-height:1.4;color:{{.C "faint"}};">In <span style="color:{{.C "muted"}};">{{.Context}}</span></p>{{end}}
      <table role="presentation" cellpadding="0" cellspacing="0"><tr>
        {{if .Mono}}<td style="vertical-align:middle;width:44px;">
          <div style="width:44px;height:44px;border-radius:50%;background:{{.MonoColor}};color:#ffffff;font-size:17px;font-weight:700;line-height:44px;text-align:center;">{{.Mono}}</div>
        </td>{{end}}
        <td style="vertical-align:middle;padding-left:14px;">
          <div style="font-size:15px;line-height:1.35;font-weight:700;color:{{.C "text"}};">{{.Actor}}</div>
          <div style="font-size:13px;line-height:1.35;color:{{.C "muted"}};margin-top:2px;">{{.Action}}</div>
        </td>
      </tr></table>
    </td></tr>
    <tr><td style="padding:18px 36px 0 36px;">
      <a href="{{.CTAURL}}" style="display:block;font-size:24px;line-height:1.3;font-weight:680;color:{{.C "text"}};text-decoration:none;">{{.Target}}</a>
    </td></tr>
    {{else}}
    <!-- transactional body: heading + intro -->
    <tr><td style="padding:30px 36px 0 36px;">
      <h1 style="margin:0;font-size:23px;line-height:1.35;font-weight:650;color:{{.C "text"}};">{{.Heading}}</h1>
    </td></tr>
    {{if .Intro}}<tr><td style="padding:13px 36px 0 36px;">
      <p style="margin:0;font-size:15px;line-height:1.6;color:{{.C "muted"}};">{{.Intro}}</p>
    </td></tr>{{end}}
    {{end}}
    {{if .Snippet}}<tr><td style="padding:20px 36px 0 36px;">
      <table role="presentation" width="100%" cellpadding="0" cellspacing="0"><tr>
        <td style="border-left:3px solid {{.C "indigo"}};background:{{.C "void"}};border:1px solid {{.C "rule"}};border-left:3px solid {{.C "indigo"}};border-radius:0 10px 10px 0;padding:15px 18px;">
          <p style="margin:0;font-size:14px;line-height:1.65;color:{{.C "quote"}};">{{.Snippet}}</p>
        </td>
      </tr></table>
    </td></tr>{{end}}
    {{if .Diff}}<tr><td style="padding:20px 36px 0 36px;">
      <table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="border:1px solid {{.C "rule"}};border-radius:10px;overflow:hidden;">
        <tr><td style="padding:10px 16px;background:{{.C "panel"}};border-bottom:1px solid {{.C "rule"}};font-size:11px;font-weight:700;letter-spacing:0.06em;text-transform:uppercase;color:{{.C "faint"}};">What changed{{if .DiffStat}} <span style="color:{{.C "muted"}};font-weight:600;letter-spacing:0;text-transform:none;">· {{.DiffStat}}</span>{{end}}</td></tr>
        {{range .Diff}}<tr><td style="padding:4px 16px;font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;font-size:13px;line-height:1.5;white-space:pre-wrap;word-break:break-word;background:{{if .Add}}#16271d{{else}}#2a181f{{end}};color:{{if .Add}}#86efac{{else}}#fca5b5{{end}};">{{if .Add}}+{{else}}−{{end}} {{.Text}}</td></tr>
        {{end}}
        {{if .DiffMore}}<tr><td style="padding:9px 16px;background:{{.C "panel"}};border-top:1px solid {{.C "rule"}};font-size:12px;color:{{.C "faint"}};">{{.DiffMore}}</td></tr>{{end}}
      </table>
    </td></tr>{{end}}
    <tr><td style="padding:26px 36px 2px 36px;">
      <a href="{{.CTAURL}}" style="display:inline-block;background:{{.C "indigo"}};color:#ffffff;text-decoration:none;font-size:15px;font-weight:600;padding:13px 26px;border-radius:9px;">{{.CTALabel}}</a>
    </td></tr>
    {{if .ShowPaste}}<tr><td style="padding:18px 36px 0 36px;">
      <p style="margin:0;font-size:13px;line-height:1.5;color:{{.C "faint"}};">Or paste this link into your browser:<br>
        <a href="{{.CTAURL}}" style="color:{{.C "link"}};word-break:break-all;">{{.CTAURL}}</a></p>
    </td></tr>{{end}}
    {{if .Related}}<tr><td style="padding:30px 36px 0 36px;">
      <p style="margin:0 0 10px 0;font-size:11px;font-weight:700;letter-spacing:0.07em;text-transform:uppercase;color:{{.C "faint"}};">Related in this wiki</p>
      <table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="border:1px solid {{.C "rule"}};border-radius:10px;overflow:hidden;">
        {{range $i, $r := .Related}}<tr><td style="padding:13px 16px;{{if $i}}border-top:1px solid {{$.C "rule"}};{{end}}">
          <a href="{{$r.URL}}" style="font-size:14px;line-height:1.4;font-weight:500;color:{{$.C "link"}};text-decoration:none;">{{$r.Label}}</a>
        </td></tr>{{end}}
      </table>
    </td></tr>{{end}}
    <tr><td style="height:32px;line-height:32px;font-size:0;">&nbsp;</td></tr>
    <!-- footer panel -->
    <tr><td style="padding:24px 36px 28px 36px;background:{{.C "panel"}};border-top:1px solid {{.C "rule"}};">
      <p style="margin:0 0 12px 0;font-size:13px;line-height:1.5;font-weight:600;color:{{.C "muted"}};">{{.Tagline}}</p>
      <p style="margin:0 0 14px 0;font-size:13px;line-height:1.5;">
        {{if .LogoOrigin}}<a href="{{.LogoOrigin}}" style="color:{{.C "link"}};text-decoration:none;">Open tela</a>{{end}}
        {{if and .LogoOrigin .ManageURL}}<span style="color:{{.C "rule"}};">&nbsp;·&nbsp;</span>{{end}}
        {{if .ManageURL}}<a href="{{.ManageURL}}" style="color:{{.C "link"}};text-decoration:none;">Notification settings</a>{{end}}
      </p>
      <p style="margin:0 0 10px 0;font-size:12px;line-height:1.5;color:{{.C "faint"}};">{{.Footer}}{{if .ManageURL}} <a href="{{.ManageURL}}" style="color:{{.C "link"}};">Manage notification emails</a>.{{end}}</p>
      <p style="margin:0;font-size:11px;line-height:1.5;color:{{.C "faint"}};">© 2026 tela</p>
    </td></tr>
  </table>
</td></tr>
</table>
</body></html>`))

// renderHTML executes the shared template into a string.
func renderHTML(v emailView) string {
	v.Tagline = emailTagline
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
		if v.Context != "" {
			b.WriteString("In " + v.Context + "\n")
		}
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
	if len(v.Diff) > 0 {
		b.WriteString("\nWhat changed")
		if v.DiffStat != "" {
			b.WriteString(" (" + v.DiffStat + ")")
		}
		b.WriteString(":\n")
		for _, dl := range v.Diff {
			sign := "−"
			if dl.Add {
				sign = "+"
			}
			b.WriteString("  " + sign + " " + dl.Text + "\n")
		}
		if v.DiffMore != "" {
			b.WriteString("  " + v.DiffMore + "\n")
		}
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
	b.WriteString("\n\n—\n" + emailTagline + "\n© 2026 tela\n")
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
		Context:    n.Context,
		Snippet:    n.Snippet,
		Diff:       n.Diff,
		DiffStat:   n.DiffStat,
		DiffMore:   n.DiffMore,
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
