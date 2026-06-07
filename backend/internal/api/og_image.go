package api

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// OG card layout: a 1200×630 canvas with an 80px inset, drawn against a fixed
// dark-mode palette. RGBs are hardcoded so the renderer never grows a
// tokens-equivalent file or a dependency on the FE's tokens.css.
const (
	ogCanvasWidth   = 1200
	ogCanvasHeight  = 630
	ogMargin        = 80
	ogDrawableWidth = ogCanvasWidth - 2*ogMargin
	ogAccentY       = ogMargin
	ogAccentWidth   = 80
	ogAccentHeight  = 4
	ogTitleSize     = 72
	ogTitleLineH    = 88
	ogSubtitleSize  = 36
	ogFooterSize    = 28
	ogMaxTitleLines = 3
)

var (
	ogBgColor       = color.RGBA{R: 0x0f, G: 0x17, B: 0x2a, A: 0xff}
	ogTitleColor    = color.RGBA{R: 0xf1, G: 0xf5, B: 0xf9, A: 0xff}
	ogSubtitleColor = color.RGBA{R: 0x94, G: 0xa3, B: 0xb8, A: 0xff}
	ogFooterColor   = color.RGBA{R: 0xcb, G: 0xd5, B: 0xe1, A: 0xff}
	ogAccentColor   = color.RGBA{R: 0x3b, G: 0x82, B: 0xf6, A: 0xff}
)

// *opentype.Font is concurrent-safe per its docstring; the per-request
// *opentype.Face wrapper is not, so we keep the parsed fonts here and build
// faces per render in renderOGImage.
var (
	ogBoldFont    *opentype.Font
	ogRegularFont *opentype.Font
)

func init() {
	bold, err := opentype.Parse(gobold.TTF)
	if err != nil {
		panic("og_image: parse gobold: " + err.Error())
	}
	regular, err := opentype.Parse(goregular.TTF)
	if err != nil {
		panic("og_image: parse goregular: " + err.Error())
	}
	ogBoldFont = bold
	ogRegularFont = regular
}

// HandleOGImage returns the server-rendered 1200×630 PNG share card for a page.
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
		title     string
		spaceName string
		updatedAt string
	)
	err = s.DB.QueryRowContext(r.Context(),
		`SELECT p.title, sp.name, p.updated_at
		   FROM pages p
		   JOIN spaces sp ON sp.id = p.space_id
		  WHERE p.id = $1 AND p.deleted_at IS NULL`, pageID,
	).Scan(&title, &spaceName, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		writeNotFoundHTML(w)
		return
	}
	if err != nil {
		log.Printf("og_image: load page %d: %v", pageID, err)
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
	etag := fmt.Sprintf(`W/"og-%d-%d"`, pageID, updatedUnix)

	if r.Header.Get("If-None-Match") == etag {
		w.Header().Set("ETag", etag)
		w.Header().Set("Cache-Control", "public, max-age=3600")
		w.WriteHeader(http.StatusNotModified)
		return
	}

	pngBytes, err := renderOGImage(title, spaceName)
	if err != nil {
		log.Printf("og_image: render page %d: %v", pageID, err)
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

// renderOGImage paints a 1200×630 RGBA card and returns PNG-encoded bytes.
// Pure function — no DB / no http — so tests can drive it directly.
//
// Builds three opentype.Face values per call because opentype.Face is
// documented as not safe for concurrent use; sharing a Face across goroutines
// races on its internal sfnt.Buffer / vector.Rasterizer / mask. The parsed
// *opentype.Font values are concurrent-safe and live at package scope.
func renderOGImage(title, spaceName string) ([]byte, error) {
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

	draw.Draw(img, img.Bounds(), &image.Uniform{C: ogBgColor}, image.Point{}, draw.Src)

	accentRect := image.Rect(
		ogMargin, ogAccentY,
		ogMargin+ogAccentWidth, ogAccentY+ogAccentHeight,
	)
	draw.Draw(img, accentRect, &image.Uniform{C: ogAccentColor}, image.Point{}, draw.Src)

	titleY := ogAccentY + ogAccentHeight + 16 + ogTitleSize
	titleLines := wrapLines(titleFace, title, ogDrawableWidth, ogMaxTitleLines)
	titleDrawer := &font.Drawer{
		Dst:  img,
		Src:  &image.Uniform{C: ogTitleColor},
		Face: titleFace,
	}
	for i, line := range titleLines {
		titleDrawer.Dot = fixed.P(ogMargin, titleY+i*ogTitleLineH)
		titleDrawer.DrawString(line)
	}

	subtitle := "in " + spaceName
	subtitle = truncateToWidth(subtitleFace, subtitle, ogDrawableWidth)
	subtitleY := titleY + (len(titleLines)-1)*ogTitleLineH + 24 + ogSubtitleSize
	subtitleDrawer := &font.Drawer{
		Dst:  img,
		Src:  &image.Uniform{C: ogSubtitleColor},
		Face: subtitleFace,
		Dot:  fixed.P(ogMargin, subtitleY),
	}
	subtitleDrawer.DrawString(subtitle)

	footerY := ogCanvasHeight - ogMargin
	footerDrawer := &font.Drawer{
		Dst:  img,
		Src:  &image.Uniform{C: ogFooterColor},
		Face: footerFace,
		Dot:  fixed.P(ogMargin, footerY),
	}
	footerDrawer.DrawString("tela")

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
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
