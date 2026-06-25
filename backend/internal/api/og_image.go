package api

import (
	"bytes"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg" // encode the share image; its init also registers the JPEG decoder
	"image/png"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// Geist (Vercel, OFL) — tela's brand typeface per DESIGN.md (Geist family only).
// Static 400/700 instances of the variable font, embedded so the renderer stays
// pure-Go with no runtime font dependency. See fonts/OFL.txt.
//
//go:embed fonts/Geist-Regular.ttf
var geistRegularTTF []byte

//go:embed fonts/Geist-Bold.ttf
var geistBoldTTF []byte

// ogShareMaxWidth bounds the deck cover used as a share image. A deck slide
// renders at ~1960×1104 (a ~1.8 MB PNG of a full-bleed, often photographic
// slide) — too heavy for link-preview fetchers that cap the image (WhatsApp
// drops previews over a few hundred KB). Downscaling to 1200-wide + JPEG gets
// it to ~100–150 KB while staying crisp at OG render sizes.
const ogShareMaxWidth = 1200

// OG card layout: a 1200×630 canvas with an 80px inset. Palette is tela's
// DESIGN.md tokens (brand-hue 277 indigo) converted OKLCH→sRGB and hardcoded, so
// the renderer stays pure-Go with no dependency on the FE's tokens.css.
const (
	ogCanvasWidth   = 1200
	ogCanvasHeight  = 630
	ogMargin        = 80
	ogDrawableWidth = ogCanvasWidth - 2*ogMargin
	ogAccentY       = ogMargin
	ogAccentWidth   = 64
	ogAccentHeight  = 5
	ogTitleSize     = 76
	ogTitleLineH    = 90
	ogSubtitleSize  = 34
	ogFooterSize    = 27
	ogMaxTitleLines = 3
	ogLogoMaxWidth  = 440 // org logo bounding box in the branded card header
	ogLogoMaxHeight = 68
	ogWeaveCell     = 44 // woven-grid pitch (tela = loom; the signature device)
	ogKickerSize    = 26 // accent eyebrow (e.g. "ASK YOUR DOCS") above the title
)

// ogAccentTintWeight is how much the org accent bleeds into the dark ink
// background on a branded card. Kept low so the surface stays a dark void and
// light title/subtitle text remains legible against any accent.
const ogAccentTintWeight = 0.14

var (
	ogBgColor       = color.RGBA{R: 0x0b, G: 0x0d, B: 0x15, A: 0xff} // ink-000 void
	ogBgTop         = color.RGBA{R: 0x13, G: 0x15, B: 0x22, A: 0xff} // subtle top of the gradient
	ogTitleColor    = color.RGBA{R: 0xf1, G: 0xf3, B: 0xfc, A: 0xff} // text-900
	ogSubtitleColor = color.RGBA{R: 0xb4, G: 0xb7, B: 0xc2, A: 0xff} // text-700
	ogFooterColor   = color.RGBA{R: 0x8f, G: 0x91, B: 0x9f, A: 0xff} // text-500
	ogRuleColor     = color.RGBA{R: 0x2b, G: 0x2d, B: 0x38, A: 0xff} // line-rule hairline
	ogWeaveColor    = color.RGBA{R: 0x1b, G: 0x1c, B: 0x24, A: 0xff} // dim woven thread
	ogAccentColor   = color.RGBA{R: 0x52, G: 0x4c, B: 0xe3, A: 0xff} // indigo-fill (tela brand)
)

// *opentype.Font is concurrent-safe per its docstring; the per-request
// *opentype.Face wrapper is not, so we keep the parsed fonts here and build
// faces per render in renderOGCard.
var (
	ogBoldFont    *opentype.Font
	ogRegularFont *opentype.Font
)

func init() {
	bold, err := opentype.Parse(geistBoldTTF)
	if err != nil {
		panic("og_image: parse Geist-Bold: " + err.Error())
	}
	regular, err := opentype.Parse(geistRegularTTF)
	if err != nil {
		panic("og_image: parse Geist-Regular: " + err.Error())
	}
	ogBoldFont = bold
	ogRegularFont = regular
}

// HandleOGImage returns the share image for a page: a deck's first slide
// (downscaled + JPEG via shrinkShareImage), else the server-rendered 1200×630
// PNG title card.
// Public — middleware bypasses /p/* and this route is NOT UA-gated because
// image fetchers (Slack, Twitter, Discord, link-preview proxies) carry
// arbitrary or empty UAs; blocking them would break the OG card path for half
// the real-world traffic.
func (s *Server) HandleOGImage(w http.ResponseWriter, r *http.Request) {
	raw := r.PathValue("id")
	pageID, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || pageID <= 0 {
		writeNotFoundHTML(w)
		return
	}

	var (
		title      string
		spaceName  string
		updatedAt  string
		body       string
		propsRaw   []byte
		spaceID    int64
		ownerOrgID int64 // NULL space.org_id scans as 0 via COALESCE
	)
	err = s.DB.QueryRowContext(r.Context(),
		`SELECT p.title, sp.name, p.updated_at, p.body, p.props, p.space_id, COALESCE(sp.org_id, 0)
		   FROM pages p
		   JOIN spaces sp ON sp.id = p.space_id
		  WHERE p.id = $1 AND p.deleted_at IS NULL`, pageID,
	).Scan(&title, &spaceName, &updatedAt, &body, &propsRaw, &spaceID, &ownerOrgID)
	if errors.Is(err, sql.ErrNoRows) {
		writeNotFoundHTML(w)
		return
	}
	if err != nil {
		slog.Error("og_image: load page", "page_id", pageID, "err", err)
		writeInternalHTML(w)
		return
	}

	// Weak ETag: rendering is deterministic in principle, but font-library bytes
	// can vary across builds, so the weak form is the honest one. Key on the
	// page's updated_at unix second, which bumps on every body/title edit.
	var updatedUnix int64
	if t, perr := time.Parse(sinceLayout, updatedAt); perr == nil {
		updatedUnix = t.Unix()
	}
	// Resolve the org brand (logo/accent/name) for this card. Folding its
	// signature into the ETag busts caches when an org changes its branding.
	brand := s.resolveOGBrand(r, ownerOrgID)
	etag := fmt.Sprintf(`W/"og-%d-%d-%s"`, pageID, updatedUnix, brand.sig)

	if r.Header.Get("If-None-Match") == etag {
		w.Header().Set("ETag", etag)
		w.Header().Set("Cache-Control", "public, max-age=3600")
		w.WriteHeader(http.StatusNotModified)
		return
	}

	// A deck's share image is its first slide (its visual identity), for public
	// AND private decks. Best-effort + time-bounded — fall back to the generic card
	// if the cover render is slow or unavailable so crawlers always get something.
	if isDeckBag(decodeProps(propsRaw)) {
		if raw, ct, ok := s.deckCoverPNG(r.Context(), body, decodeProps(propsRaw), spaceID); ok {
			img, ict := shrinkShareImage(raw, ct)
			w.Header().Set("Content-Type", ict)
			w.Header().Set("Cache-Control", "public, max-age=3600")
			w.Header().Set("ETag", etag)
			w.Header().Set("Content-Length", strconv.Itoa(len(img)))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(img)
			return
		}
	}

	pngBytes, err := renderOGCard(title, "in "+spaceName, brand)
	if err != nil {
		slog.Error("og_image: render page", "page_id", pageID, "err", err)
		writeInternalHTML(w)
		return
	}

	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Header().Set("ETag", etag)
	w.Header().Set("Content-Length", strconv.Itoa(len(pngBytes)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(pngBytes)
}

// shrinkShareImage downscales a deck cover to a link-preview-friendly size and
// re-encodes it as JPEG (deck slides are photographic — JPEG is far smaller than
// PNG). Returns the bytes + content-type to serve. Best-effort: if decode or
// re-encode fails, or the source is already small enough, it returns the
// original bytes/ct unchanged so the OG path never breaks on an odd cover.
func shrinkShareImage(raw []byte, ct string) ([]byte, string) {
	src, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return raw, ct
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return raw, ct
	}
	// Fit within ogShareMaxWidth, preserving aspect; never upscale.
	dw, dh := w, h
	if w > ogShareMaxWidth {
		dw = ogShareMaxWidth
		dh = h * ogShareMaxWidth / w
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, b, xdraw.Over, nil)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 82}); err != nil {
		return raw, ct
	}
	// Keep the original if our re-encode somehow came out larger (e.g. a tiny
	// source that was already optimally compressed).
	if buf.Len() >= len(raw) {
		return raw, ct
	}
	return buf.Bytes(), "image/jpeg"
}

// renderOGImage paints the unbranded 1200×630 card. Thin wrapper over
// renderOGCard with the zero brand — kept for callers/tests that don't carry an
// org brand. subtitle is rendered verbatim (page cards pass "in <space>").
func renderOGImage(title, subtitle string) ([]byte, error) {
	return renderOGCard(title, subtitle, ogBrand{})
}

// renderOGCard paints a 1200×630 RGBA card and returns PNG-encoded bytes. With
// the zero brand it renders the default dark tela card (blue accent bar, "tela"
// footer); with a brand it tints the background toward the org accent, draws the
// org logo in the header, and footers the org name — the full-brand white-label
// card. Pure function — no DB / no http — so tests can drive it directly.
//
// Builds three opentype.Face values per call because opentype.Face is
// documented as not safe for concurrent use; sharing a Face across goroutines
// races on its internal sfnt.Buffer / vector.Rasterizer / mask. The parsed
// *opentype.Font values are concurrent-safe and live at package scope.
// ogCardOpts configures a rendered card. kicker is an accent eyebrow above the
// title that frames the card as an action (e.g. "ASK YOUR DOCS") rather than an
// article; maxTitleLines caps the title wrap (0 → ogMaxTitleLines); brand carries
// the org logo/accent/name.
type ogCardOpts struct {
	kicker        string
	title         string
	subtitle      string
	maxTitleLines int
	brand         ogBrand
}

// renderOGCard renders a plain card (no kicker, default title wrap).
func renderOGCard(title, subtitle string, brand ogBrand) ([]byte, error) {
	return renderOGCardOpts(ogCardOpts{title: title, subtitle: subtitle, brand: brand})
}

func renderOGCardOpts(o ogCardOpts) ([]byte, error) {
	titleFace, err := opentype.NewFace(ogBoldFont, &opentype.FaceOptions{
		Size: ogTitleSize, DPI: 72, Hinting: font.HintingFull,
	})
	if err != nil {
		return nil, fmt.Errorf("og: title face: %w", err)
	}
	defer titleFace.Close()

	subtitleFace, err := opentype.NewFace(ogRegularFont, &opentype.FaceOptions{
		Size: ogSubtitleSize, DPI: 72, Hinting: font.HintingFull,
	})
	if err != nil {
		return nil, fmt.Errorf("og: subtitle face: %w", err)
	}
	defer subtitleFace.Close()

	footerFace, err := opentype.NewFace(ogBoldFont, &opentype.FaceOptions{
		Size: ogFooterSize, DPI: 72, Hinting: font.HintingFull,
	})
	if err != nil {
		return nil, fmt.Errorf("og: footer face: %w", err)
	}
	defer footerFace.Close()

	img := image.NewRGBA(image.Rect(0, 0, ogCanvasWidth, ogCanvasHeight))

	// The accent is tela's indigo by default, or the org's accent when branded —
	// the single earned color across the whole card (woven threads, light sweep,
	// the header mark/rule).
	accent := ogAccentColor
	if o.brand.hasAccent {
		accent = o.brand.accent
	}
	paintOGBackground(img, accent, o.brand.hasAccent)

	// Header cursor: optional org logo, then an accent kicker eyebrow (when set),
	// else a short accent rule (the plain page-card look). The title sits below.
	top := ogMargin
	if o.brand.logo != nil {
		top += drawLogoFit(img, o.brand.logo, ogMargin, ogMargin, ogLogoMaxWidth, ogLogoMaxHeight)
	}
	var titleY int
	if o.kicker != "" {
		kickerFace, kerr := opentype.NewFace(ogBoldFont, &opentype.FaceOptions{
			Size: ogKickerSize, DPI: 72, Hinting: font.HintingFull,
		})
		if kerr != nil {
			return nil, fmt.Errorf("og: kicker face: %w", kerr)
		}
		defer kickerFace.Close()
		ky := ogMargin + ogKickerSize
		if o.brand.logo != nil {
			ky = top + 26 + ogKickerSize
		}
		mk := ogKickerSize - 7
		draw.Draw(img, image.Rect(ogMargin, ky-mk, ogMargin+mk, ky), &image.Uniform{C: accent}, image.Point{}, draw.Src)
		kd := &font.Drawer{Dst: img, Src: &image.Uniform{C: accent}, Face: kickerFace, Dot: fixed.P(ogMargin+mk+14, ky)}
		kd.DrawString(strings.ToUpper(o.kicker))
		titleY = ky + 30 + ogTitleSize
	} else if o.brand.logo != nil {
		titleY = top + 44 + ogTitleSize
	} else {
		draw.Draw(img, image.Rect(ogMargin, ogAccentY, ogMargin+ogAccentWidth, ogAccentY+ogAccentHeight),
			&image.Uniform{C: accent}, image.Point{}, draw.Src)
		titleY = ogAccentY + ogAccentHeight + 34 + ogTitleSize
	}

	maxLines := o.maxTitleLines
	if maxLines <= 0 {
		maxLines = ogMaxTitleLines
	}
	titleLines := wrapLines(titleFace, o.title, ogDrawableWidth, maxLines)
	titleDrawer := &font.Drawer{
		Dst:  img,
		Src:  &image.Uniform{C: ogTitleColor},
		Face: titleFace,
	}
	for i, line := range titleLines {
		titleDrawer.Dot = fixed.P(ogMargin, titleY+i*ogTitleLineH)
		titleDrawer.DrawString(line)
	}

	// Draw the subtitle only if it clears the footer rule — a long title (esp.
	// under a logo) can push it into the footer; there the title is the message
	// and the subtitle is dropped rather than colliding. ruleY mirrors the footer.
	subtitleY := titleY + (len(titleLines)-1)*ogTitleLineH + 24 + ogSubtitleSize
	ruleY := ogCanvasHeight - ogMargin - ogFooterSize - 20
	if o.subtitle != "" && subtitleY <= ruleY-12 {
		sub := truncateToWidth(subtitleFace, o.subtitle, ogDrawableWidth)
		subtitleDrawer := &font.Drawer{
			Dst:  img,
			Src:  &image.Uniform{C: ogSubtitleColor},
			Face: subtitleFace,
			Dot:  fixed.P(ogMargin, subtitleY),
		}
		subtitleDrawer.DrawString(sub)
	}

	// Footer: a hairline rule, then a small accent mark + the wordmark (org name
	// when branded, else "tela"). The rule + mark anchor the lower third so the
	// card doesn't read as an empty void below the title.
	footer := "tela"
	if o.brand.name != "" {
		footer = o.brand.name
	}
	footerY := ogCanvasHeight - ogMargin
	draw.Draw(img, image.Rect(ogMargin, ruleY, ogCanvasWidth-ogMargin, ruleY+2),
		&image.Uniform{C: ogRuleColor}, image.Point{}, draw.Src)

	const mark = 22
	draw.Draw(img, image.Rect(ogMargin, footerY-mark, ogMargin+mark, footerY),
		&image.Uniform{C: accent}, image.Point{}, draw.Src)

	footerX := ogMargin + mark + 18
	footer = truncateToWidth(footerFace, footer, ogDrawableWidth-mark-18)
	footerDrawer := &font.Drawer{
		Dst:  img,
		Src:  &image.Uniform{C: ogFooterColor},
		Face: footerFace,
		Dot:  fixed.P(footerX, footerY),
	}
	footerDrawer.DrawString(footer)

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// paintOGBackground draws tela's signature atmosphere: a near-black ink gradient,
// the faint woven grid (tela = loom), and one diagonal light sweep tinted by the
// accent. On a branded card the ink is tinted faintly toward the org accent. This
// is what turns the card from a flat void into something deliberate.
func paintOGBackground(img *image.RGBA, accent color.RGBA, branded bool) {
	top, bot := ogBgTop, ogBgColor
	if branded {
		top = mixRGBA(accent, ogBgTop, ogAccentTintWeight)
		bot = mixRGBA(accent, ogBgColor, ogAccentTintWeight)
	}

	// Vertical ink gradient (lighter at the top).
	for y := 0; y < ogCanvasHeight; y++ {
		t := float64(y) / float64(ogCanvasHeight-1)
		c := mixRGBA(bot, top, t) // t=0 → top, t=1 → bot
		draw.Draw(img, image.Rect(0, y, ogCanvasWidth, y+1), &image.Uniform{C: c}, image.Point{}, draw.Src)
	}

	// Woven grid — warp (vertical) + weft (horizontal) threads, faint and uniform.
	for x := ogWeaveCell; x < ogCanvasWidth; x += ogWeaveCell {
		draw.Draw(img, image.Rect(x, 0, x+1, ogCanvasHeight), &image.Uniform{C: ogWeaveColor}, image.Point{}, draw.Src)
	}
	for y := ogWeaveCell; y < ogCanvasHeight; y += ogWeaveCell {
		draw.Draw(img, image.Rect(0, y, ogCanvasWidth, y+1), &image.Uniform{C: ogWeaveColor}, image.Point{}, draw.Src)
	}

	// One diagonal light sweep — a single luminance gesture, near-white with a
	// faint accent tint, lighting the upper-left through the title (and catching
	// the threads it crosses). Subtle peak so it reads as atmosphere, not a blob.
	light := mixRGBA(color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}, accent, 0.62)
	const center, sigma, peak = 360.0, 300.0, 0.13
	for y := 0; y < ogCanvasHeight; y++ {
		for x := 0; x < ogCanvasWidth; x++ {
			d := (float64(x)*0.8 + float64(y)*0.55) - center
			inten := peak * math.Exp(-(d*d)/(2*sigma*sigma))
			if inten < 0.003 {
				continue
			}
			i := img.PixOffset(x, y)
			img.Pix[i+0] = mixU8(img.Pix[i+0], light.R, inten)
			img.Pix[i+1] = mixU8(img.Pix[i+1], light.G, inten)
			img.Pix[i+2] = mixU8(img.Pix[i+2], light.B, inten)
		}
	}
}

// mixRGBA returns base shifted t of the way toward over (t in [0,1]), opaque.
func mixRGBA(over, base color.RGBA, t float64) color.RGBA {
	return color.RGBA{
		R: mixU8(base.R, over.R, t),
		G: mixU8(base.G, over.G, t),
		B: mixU8(base.B, over.B, t),
		A: 0xff,
	}
}

func mixU8(base, over uint8, t float64) uint8 {
	v := float64(base)*(1-t) + float64(over)*t
	if v > 255 {
		return 255
	}
	if v < 0 {
		return 0
	}
	return uint8(v + 0.5)
}

// drawLogoFit composites a logo into dst at (x,y), scaled to fit within
// maxW×maxH while preserving aspect ratio (never upscaled past the box). Returns
// the drawn height so the caller can place the title below it. Drawn with Over
// so a transparent logo blends onto the tinted background.
func drawLogoFit(dst *image.RGBA, logo image.Image, x, y, maxW, maxH int) int {
	b := logo.Bounds()
	lw, lh := b.Dx(), b.Dy()
	if lw <= 0 || lh <= 0 {
		return 0
	}
	dw, dh := lw, lh
	// Scale down to fit the height, then clamp the width.
	if dh > maxH {
		dw = dw * maxH / dh
		dh = maxH
	}
	if dw > maxW {
		dh = dh * maxW / dw
		dw = maxW
	}
	rect := image.Rect(x, y, x+dw, y+dh)
	xdraw.CatmullRom.Scale(dst, rect, logo, b, xdraw.Over, nil)
	return dh
}

// wrapLines greedily wraps text into at most maxLines lines that fit within
// maxWidth pixels when drawn with face. The final line is suffixed with "…" if
// remaining words would overflow. Whitespace-separated; existing newlines
// inside the input are flattened to single spaces by the upstream caller (page
// title is a single TEXT column with no newline convention).
func wrapLines(face font.Face, text string, maxWidth, maxLines int) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return []string{""}
	}
	if maxLines <= 0 {
		return nil
	}

	words := strings.Fields(text)
	maxFixed := fixed.I(maxWidth)

	lines := make([]string, 0, maxLines)
	cur := ""
	i := 0
	for i < len(words) && len(lines) < maxLines {
		candidate := cur
		if candidate == "" {
			candidate = words[i]
		} else {
			candidate = cur + " " + words[i]
		}
		if font.MeasureString(face, candidate) <= maxFixed {
			cur = candidate
			i++
			continue
		}
		// Adding this word overflowed.
		if cur == "" {
			// A single word longer than the line; force it onto its own line
			// and truncate with ellipsis. Avoid getting stuck.
			lines = append(lines, truncateToWidth(face, words[i], maxWidth))
			i++
			continue
		}
		lines = append(lines, cur)
		cur = ""
	}
	if cur != "" && len(lines) < maxLines {
		lines = append(lines, cur)
		cur = ""
	}

	// Words left over: the final line must collapse them with an ellipsis.
	if i < len(words) {
		if len(lines) == 0 {
			// maxLines was 0 or the very first word didn't fit even truncated.
			return []string{truncateToWidth(face, strings.Join(words[i:], " "), maxWidth)}
		}
		last := lines[len(lines)-1]
		remainder := last + " " + strings.Join(words[i:], " ")
		lines[len(lines)-1] = truncateToWidth(face, remainder, maxWidth)
	}

	return lines
}

// truncateToWidth returns s if it fits within maxWidth, else the longest
// rune-prefix that fits with a trailing "…" appended.
func truncateToWidth(face font.Face, s string, maxWidth int) string {
	maxFixed := fixed.I(maxWidth)
	if font.MeasureString(face, s) <= maxFixed {
		return s
	}
	runes := []rune(s)
	ellipsis := "…"
	for n := len(runes) - 1; n >= 0; n-- {
		candidate := strings.TrimRight(string(runes[:n]), " ") + ellipsis
		if font.MeasureString(face, candidate) <= maxFixed {
			return candidate
		}
	}
	return ellipsis
}
