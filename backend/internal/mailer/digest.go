package mailer

import (
	"bytes"
	"html/template"
	"strings"
)

// Digest is tela's periodic (weekly) recap email — "here's what moved across the
// spaces you can see, and what needs your eyes." It's a richer layout than the
// shared verify/reset/notification template (stat tiles + sections + badges), so
// it carries its own template, reusing only the brand palette + white-label
// Brand. Data is assembled by the API layer (internal/api/digest.go) from
// signals tela already records — events, comments, page revisions, Atlas
// staleness — and passed in as DigestData; this file only renders.

// DigestStats are the headline counts for the period.
type DigestStats struct {
	Updated    int
	New        int
	Comments   int
	NewMembers int
}

// DigestUpdate is one "new & updated" row. Badge is "" | "APPROVED" | "ATLAS".
type DigestUpdate struct {
	Title     string
	SpaceName string
	Actor     string // "Maya edited" / "auto-refreshed" / "Deniz created"
	When      string // "2 days ago"
	Summary   string // one-line change summary (optional)
	URL       string
	Badge     string
}

// DigestAttention is one "needs your eyes" row. Kind is a short pill label, e.g.
// "STALE 6w" or "OPEN Q"; Tone is "warn" (amber) or "info" (indigo).
type DigestAttention struct {
	Kind   string
	Tone   string
	Title  string
	Detail string
	URL    string
}

// DigestData is the full rendering model for one recipient's digest.
type DigestData struct {
	Greeting   string // first name / username, "" → no name
	DateRange  string // "Jun 24 – Jun 30, 2026"
	SpaceCount int
	Gist       string // AI one-paragraph summary (optional; "" hides the callout)
	Stats      DigestStats
	Updates    []DigestUpdate
	MoreCount  int // "+N more updates" (0 hides)
	Attention  []DigestAttention
	AppURL     string // base app URL — CTA + logo + link origin
	PrefsURL   string // notification preferences
	UnsubURL   string // one-click unsubscribe
	Brand      Brand  // per-org white-label (zero → tela)
}

// digestView is the template model — DigestData plus resolved brand chrome.
type digestView struct {
	D         DigestData
	BrandName string
	LogoURL   string // absolute logo (org logo or tela icon); "" → wordmark only
	Tagline   string
	Powered   bool
	// resolved palette (accent may be white-labeled)
	Indigo, Text, Muted, Faint, Panel, Rule, Card, Pill string
}

// Digest renders the weekly digest for one recipient.
func Digest(to, subject string, d DigestData) Message {
	v := digestView{
		D:      d,
		Indigo: clrIndigo,
		Text:   clrText,
		Muted:  clrMuted,
		Faint:  clrFaint,
		Panel:  clrPanel,
		Rule:   clrRule,
		Card:   clrCard,
		Pill:   clrPill,
	}
	// White-label: org name + accent + logo, mirroring emailView.applyBrand.
	name := strings.TrimSpace(d.Brand.Name)
	if name != "" && name != "tela" {
		v.BrandName = name
		v.LogoURL = d.Brand.LogoURL
		v.Powered = true
		if d.Brand.Accent != "" {
			v.Indigo = d.Brand.Accent
		}
	} else {
		v.BrandName = "tela"
		if d.AppURL != "" {
			v.LogoURL = strings.TrimRight(d.AppURL, "/") + "/icon-64.png"
		}
	}
	if v.Powered {
		v.Tagline = "Powered by tela."
	} else {
		v.Tagline = emailTagline
	}

	var buf bytes.Buffer
	_ = digestTmpl.Execute(&buf, v)
	return Message{To: to, Subject: subject, HTML: buf.String(), Text: renderDigestText(d)}
}

func renderDigestText(d DigestData) string {
	var b strings.Builder
	if d.Greeting != "" {
		b.WriteString("Hi " + d.Greeting + ",\n\n")
	}
	b.WriteString("Your week (" + d.DateRange + ")\n\n")
	if d.Gist != "" {
		b.WriteString(d.Gist + "\n\n")
	}
	b.WriteString("This week: ")
	b.WriteString(itoa(d.Stats.Updated) + " updated, " + itoa(d.Stats.New) + " new, " +
		itoa(d.Stats.Comments) + " comments, " + itoa(d.Stats.NewMembers) + " new members.\n\n")
	if len(d.Updates) > 0 {
		b.WriteString("New & updated:\n")
		for _, u := range d.Updates {
			b.WriteString("- " + u.Title + " (" + u.SpaceName + " · " + u.Actor + " · " + u.When + ")\n")
			if u.Summary != "" {
				b.WriteString("  " + u.Summary + "\n")
			}
			if u.URL != "" {
				b.WriteString("  " + u.URL + "\n")
			}
		}
		if d.MoreCount > 0 {
			b.WriteString("...and " + itoa(d.MoreCount) + " more.\n")
		}
		b.WriteString("\n")
	}
	if len(d.Attention) > 0 {
		b.WriteString("Needs your eyes:\n")
		for _, a := range d.Attention {
			b.WriteString("- [" + a.Kind + "] " + a.Title + "\n")
			if a.Detail != "" {
				b.WriteString("  " + a.Detail + "\n")
			}
		}
		b.WriteString("\n")
	}
	b.WriteString("Catch up: " + d.AppURL + "\n")
	return b.String()
}

// itoa avoids pulling strconv into the template path for one use.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

var digestTmpl = template.Must(template.New("digest").Parse(`<!doctype html>
<html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1.0"></head>
<body style="margin:0;padding:0;background:#f3f3f7;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;-webkit-font-smoothing:antialiased;">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="background:#f3f3f7;padding:32px 12px;"><tr><td align="center">
<table role="presentation" width="600" cellpadding="0" cellspacing="0" style="width:600px;max-width:600px;background:{{.Card}};border-radius:14px;overflow:hidden;border:1px solid #e6e6ef;">

  <tr><td style="height:4px;background:{{.Indigo}};line-height:4px;font-size:0;">&nbsp;</td></tr>

  <tr><td style="padding:26px 32px 8px 32px;">
    <table role="presentation" width="100%" cellpadding="0" cellspacing="0"><tr>
      <td align="left" style="vertical-align:middle;">
        {{if .LogoURL}}<img src="{{.LogoURL}}" width="26" height="26" alt="" style="vertical-align:middle;border-radius:7px;display:inline-block;"><span style="font-size:17px;font-weight:600;color:{{.Text}};vertical-align:middle;margin-left:9px;letter-spacing:-.01em;">{{.BrandName}}</span>{{else}}<span style="font-size:17px;font-weight:600;color:{{.Text}};letter-spacing:-.01em;">{{.BrandName}}</span>{{end}}
      </td>
      <td align="right" style="vertical-align:middle;font-size:12px;color:{{.Faint}};">{{.D.DateRange}}</td>
    </tr></table>
  </td></tr>

  <tr><td style="padding:14px 32px 0 32px;">
    <span style="display:inline-block;background:#eef0ff;color:{{.Indigo}};font-size:11px;font-weight:600;letter-spacing:.12em;text-transform:uppercase;padding:4px 10px;border-radius:20px;">Weekly digest</span>
    <h1 style="margin:14px 0 6px 0;font-size:24px;line-height:1.25;font-weight:700;color:{{.Text}};letter-spacing:-.02em;">Your week{{if .Powered}} in {{.BrandName}}{{end}}</h1>
    <p style="margin:0;font-size:15px;line-height:1.5;color:{{.Muted}};">{{if .D.Greeting}}Hi {{.D.Greeting}} — {{end}}here's what moved across the {{.D.SpaceCount}} space{{if ne .D.SpaceCount 1}}s{{end}} you can see this week.</p>
  </td></tr>

  {{if .D.Gist}}
  <tr><td style="padding:20px 32px 0 32px;">
    <table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="background:#f5f6ff;border:1px solid #e0e2ff;border-left:3px solid {{.Indigo}};border-radius:8px;"><tr><td style="padding:14px 16px;">
      <div style="font-size:11px;font-weight:600;letter-spacing:.08em;text-transform:uppercase;color:{{.Indigo}};margin-bottom:6px;">&#10022; The gist</div>
      <p style="margin:0;font-size:14px;line-height:1.55;color:#33334a;">{{.D.Gist}}</p>
    </td></tr></table>
  </td></tr>
  {{end}}

  <tr><td style="padding:22px 32px 4px 32px;">
    <table role="presentation" width="100%" cellpadding="0" cellspacing="0"><tr>
      <td width="25%" align="center" style="padding:10px 4px;background:{{.Panel}};border:1px solid #eee;border-radius:8px 0 0 8px;"><div style="font-size:22px;font-weight:700;color:{{.Text}};">{{.D.Stats.Updated}}</div><div style="font-size:11px;color:{{.Faint}};">updated</div></td>
      <td width="25%" align="center" style="padding:10px 4px;background:{{.Panel}};border:1px solid #eee;border-left:0;"><div style="font-size:22px;font-weight:700;color:{{.Text}};">{{.D.Stats.New}}</div><div style="font-size:11px;color:{{.Faint}};">new pages</div></td>
      <td width="25%" align="center" style="padding:10px 4px;background:{{.Panel}};border:1px solid #eee;border-left:0;"><div style="font-size:22px;font-weight:700;color:{{.Text}};">{{.D.Stats.Comments}}</div><div style="font-size:11px;color:{{.Faint}};">comments</div></td>
      <td width="25%" align="center" style="padding:10px 4px;background:{{.Panel}};border:1px solid #eee;border-left:0;border-radius:0 8px 8px 0;"><div style="font-size:22px;font-weight:700;color:{{.Text}};">{{if gt .D.Stats.NewMembers 0}}+{{end}}{{.D.Stats.NewMembers}}</div><div style="font-size:11px;color:{{.Faint}};">new members</div></td>
    </tr></table>
  </td></tr>

  {{if .D.Updates}}
  <tr><td style="padding:26px 32px 0 32px;">
    <div style="font-size:13px;font-weight:600;color:{{.Text}};text-transform:uppercase;letter-spacing:.05em;border-bottom:1px solid #eee;padding-bottom:8px;">New &amp; updated</div>
    <table role="presentation" width="100%" cellpadding="0" cellspacing="0">
      {{range .D.Updates}}
      <tr><td style="padding:14px 0 12px 0;border-bottom:1px solid #f2f2f6;">
        <a href="{{.URL}}" style="font-size:15px;font-weight:600;color:{{$.Indigo}};text-decoration:none;">{{.Title}}</a>
        {{if eq .Badge "APPROVED"}}<span style="display:inline-block;font-size:10px;font-weight:600;color:#0a8a55;background:#e7f7ee;padding:2px 7px;border-radius:20px;margin-left:8px;vertical-align:middle;">APPROVED</span>{{else if eq .Badge "ATLAS"}}<span style="display:inline-block;font-size:10px;font-weight:600;color:{{$.Indigo}};background:#eef0ff;padding:2px 7px;border-radius:20px;margin-left:8px;vertical-align:middle;">&#10022; ATLAS</span>{{end}}
        <div style="font-size:12.5px;color:{{$.Faint}};margin-top:3px;">{{.SpaceName}} · {{.Actor}} · {{.When}}</div>
        {{if .Summary}}<div style="font-size:13.5px;color:{{$.Muted}};margin-top:4px;line-height:1.5;">{{.Summary}}</div>{{end}}
      </td></tr>
      {{end}}
    </table>
    {{if gt .D.MoreCount 0}}<a href="{{.D.AppURL}}" style="display:inline-block;margin-top:12px;font-size:13px;color:{{.Indigo}};text-decoration:none;">+ {{.D.MoreCount}} more updates &rarr;</a>{{end}}
  </td></tr>
  {{end}}

  {{if .D.Attention}}
  <tr><td style="padding:26px 32px 0 32px;">
    <div style="font-size:13px;font-weight:600;color:{{.Text}};text-transform:uppercase;letter-spacing:.05em;padding-bottom:4px;">Needs your eyes</div>
    <table role="presentation" width="100%" cellpadding="0" cellspacing="0">
      {{range .D.Attention}}
      <tr><td style="padding:8px 0 0 0;">
        {{if eq .Tone "warn"}}
        <table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="background:#fdf9f1;border:1px solid #f0e4cc;border-left:3px solid #d99a2b;border-radius:8px;"><tr><td style="padding:12px 14px;">
          <span style="display:inline-block;font-size:10px;font-weight:700;letter-spacing:.06em;color:#9a6a00;background:#f9ecd2;padding:3px 8px;border-radius:5px;">{{.Kind}}</span>
          <div style="margin-top:7px;"><a href="{{.URL}}" style="font-size:14px;font-weight:600;color:{{$.Text}};text-decoration:none;">{{.Title}}</a></div>
          {{if .Detail}}<div style="font-size:12.5px;color:{{$.Muted}};margin-top:3px;line-height:1.45;">{{.Detail}}</div>{{end}}
        </td></tr></table>
        {{else}}
        <table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="background:#f5f6ff;border:1px solid #e2e4ff;border-left:3px solid {{$.Indigo}};border-radius:8px;"><tr><td style="padding:12px 14px;">
          <span style="display:inline-block;font-size:10px;font-weight:700;letter-spacing:.06em;color:{{$.Indigo}};background:#e8eaff;padding:3px 8px;border-radius:5px;">{{.Kind}}</span>
          <div style="margin-top:7px;"><a href="{{.URL}}" style="font-size:14px;font-weight:600;color:{{$.Text}};text-decoration:none;">{{.Title}}</a></div>
          {{if .Detail}}<div style="font-size:12.5px;color:{{$.Muted}};margin-top:3px;line-height:1.45;">{{.Detail}}</div>{{end}}
        </td></tr></table>
        {{end}}
      </td></tr>
      {{end}}
    </table>
  </td></tr>
  {{end}}

  <tr><td align="center" style="padding:30px 32px 6px 32px;">
    <a href="{{.D.AppURL}}" style="display:inline-block;background:{{.Indigo}};color:#ffffff;font-size:14px;font-weight:600;text-decoration:none;padding:12px 28px;border-radius:8px;">Catch up in {{.BrandName}} &rarr;</a>
  </td></tr>

  <tr><td style="padding:22px 32px 30px 32px;border-top:1px solid #f0f0f4;">
    <p style="margin:0 0 6px 0;font-size:12px;color:{{.Faint}};line-height:1.5;">{{.Tagline}}</p>
    <p style="margin:0;font-size:11px;color:#b4b4c6;line-height:1.6;">You're getting this weekly.{{if .D.PrefsURL}} <a href="{{.D.PrefsURL}}" style="color:{{.Faint}};">Change frequency</a>{{end}}{{if .D.UnsubURL}} · <a href="{{.D.UnsubURL}}" style="color:{{.Faint}};">Unsubscribe</a>{{end}}</p>
  </td></tr>

</table>
</td></tr></table>
</body></html>`))
