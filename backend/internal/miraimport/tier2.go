package miraimport

import (
	"encoding/json"
	"strconv"
	"strings"
)

// Tier-2 covers the 15 mira visual block types that don't map cleanly to
// markdown — charts, kanban boards, timelines, calendars, etc. Each renderer
// emits a best-effort structured-text approximation; every output gets a
// trailing tier2Footer line so the rendered tela page makes the fidelity gap
// visible.
//
// The unknown / future block type stub is intentionally NOT a Tier-2 block —
// it lives in convert.go and is its own marker (no footer).

const (
	// tier2Footer marks every Tier-2 block's output as reduced-fidelity.
	tier2Footer = "> _Imported from mira render — visual fidelity reduced._"

	// miraAssetBase is the canonical URL prefix for mira-hosted asset ids.
	// Gallery images and image blocks with file.type=asset both resolve here.
	miraAssetBase = "https://mira.cagdas.io/asset/"
)

// appendTier2Footer adds the footer line below s on its own blank-separated
// paragraph. Returns "" when s is empty so an empty placeholder doesn't bleed
// a stranded marker into the output.
func appendTier2Footer(s string) string {
	if s == "" {
		return ""
	}
	return s + "\n\n" + tier2Footer
}

// renderTier2 dispatches a Tier-2 block to its placeholder renderer. Returns
// the empty string when b.Type isn't Tier-2; the caller should fall through to
// the unknown-block stub in that case.
func renderTier2(b block) string {
	switch b.Type {
	case "chart":
		return appendTier2Footer(renderChart(b))
	case "stat_grid":
		return appendTier2Footer(renderStatGrid(b))
	case "timeline":
		return appendTier2Footer(renderTimeline(b))
	case "kanban":
		return appendTier2Footer(renderKanban(b))
	case "comparison_matrix":
		return appendTier2Footer(renderComparisonMatrix(b))
	case "tabs":
		return appendTier2Footer(renderTabs(b))
	case "columns":
		return appendTier2Footer(renderColumns(b))
	case "gallery":
		return appendTier2Footer(renderGallery(b))
	case "slides":
		return appendTier2Footer(renderSlides(b))
	case "calendar":
		return appendTier2Footer(renderCalendar(b))
	case "map":
		return appendTier2Footer(renderMap(b))
	case "video":
		return appendTier2Footer(renderVideo(b))
	case "network":
		return appendTier2Footer(renderNetwork(b))
	case "diff":
		return appendTier2Footer(renderDiff(b))
	case "choice":
		return appendTier2Footer(renderChoice(b))
	case "approve":
		return appendTier2Footer(renderApprove(b))
	}
	return ""
}

// ---- chart ----

func renderChart(b block) string {
	var body struct {
		ChartType string `json:"chart_type"`
		Title     string `json:"title"`
		XAxis     *struct {
			Label      string   `json:"label"`
			Type       string   `json:"type"`
			Categories []string `json:"categories"`
		} `json:"x_axis,omitempty"`
		YAxis *struct {
			Label string `json:"label"`
		} `json:"y_axis,omitempty"`
		Series []struct {
			Name string            `json:"name"`
			Data []json.RawMessage `json:"data"`
		} `json:"series"`
	}
	if err := decodeBody(b, "chart", &body); err != nil {
		return ""
	}
	var out strings.Builder
	if t := strings.TrimSpace(body.Title); t != "" {
		out.WriteString("### ")
		out.WriteString(escapeMarkdown(t))
		out.WriteString("\n\n")
	}

	if body.XAxis != nil && len(body.XAxis.Categories) > 0 && len(body.Series) > 0 {
		// Category-axis chart: one row per category, one column per series.
		header := []string{cellEscape(body.XAxis.Label)}
		for _, s := range body.Series {
			header = append(header, cellEscape(s.Name))
		}
		writeRow(&out, header)
		out.WriteString("\n")
		writeSeparator(&out, len(header))
		for i, cat := range body.XAxis.Categories {
			row := []string{cellEscape(cat)}
			for _, s := range body.Series {
				if i < len(s.Data) {
					row = append(row, cellEscape(stringifyJSON(s.Data[i])))
				} else {
					row = append(row, "")
				}
			}
			out.WriteString("\n")
			writeRow(&out, row)
		}
		return strings.TrimRight(out.String(), "\n")
	}

	// Non-category chart (pie/donut, time/number xy): bullet list per series.
	if len(body.Series) == 0 {
		return strings.TrimRight(out.String(), "\n")
	}
	for _, s := range body.Series {
		parts := make([]string, 0, len(s.Data))
		for _, d := range s.Data {
			parts = append(parts, stringifyJSON(d))
		}
		name := escapeMarkdown(s.Name)
		if name == "" {
			name = "series"
		}
		out.WriteString("- **")
		out.WriteString(name)
		out.WriteString(":** ")
		out.WriteString(escapeMarkdown(strings.Join(parts, ", ")))
		out.WriteString("\n")
	}
	return strings.TrimRight(out.String(), "\n")
}

// ---- stat_grid ----

func renderStatGrid(b block) string {
	var body struct {
		Title string `json:"title"`
		Tiles []struct {
			Label string          `json:"label"`
			Value json.RawMessage `json:"value"`
			Unit  string          `json:"unit"`
		} `json:"tiles"`
	}
	if err := decodeBody(b, "stat_grid", &body); err != nil {
		return ""
	}
	var out strings.Builder
	if t := strings.TrimSpace(body.Title); t != "" {
		out.WriteString("### ")
		out.WriteString(escapeMarkdown(t))
		out.WriteString("\n\n")
	}
	for _, tile := range body.Tiles {
		val := stringifyJSON(tile.Value)
		if u := strings.TrimSpace(tile.Unit); u != "" {
			val += " " + u
		}
		out.WriteString("- **")
		out.WriteString(escapeMarkdown(tile.Label))
		out.WriteString(":** ")
		out.WriteString(escapeMarkdown(val))
		out.WriteString("\n")
	}
	return strings.TrimRight(out.String(), "\n")
}

// ---- timeline ----

func renderTimeline(b block) string {
	var body struct {
		Title  string `json:"title"`
		Events []struct {
			Date  json.RawMessage `json:"date"`
			Label string          `json:"label"`
		} `json:"events"`
	}
	if err := decodeBody(b, "timeline", &body); err != nil {
		return ""
	}
	var out strings.Builder
	if t := strings.TrimSpace(body.Title); t != "" {
		out.WriteString("### ")
		out.WriteString(escapeMarkdown(t))
		out.WriteString("\n\n")
	}
	parts := make([]string, 0, len(body.Events))
	for _, e := range body.Events {
		date := timelineDateDisplay(e.Date)
		section := "### " + escapeMarkdown(date) + "\n- " + escapeMarkdown(e.Label)
		parts = append(parts, section)
	}
	out.WriteString(strings.Join(parts, "\n\n"))
	return strings.TrimRight(out.String(), "\n")
}

// timelineDateDisplay resolves the mira-spec dual-shape `date` field to a
// human display string. String form → use directly. Object form: prefer
// `display`, fall back to `sort`, then to a `start – end` range.
func timelineDateDisplay(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return ""
	}
	if d, ok := obj["display"]; ok {
		var v string
		if json.Unmarshal(d, &v) == nil && v != "" {
			return v
		}
	}
	if d, ok := obj["sort"]; ok {
		var v string
		if json.Unmarshal(d, &v) == nil && v != "" {
			return v
		}
	}
	if startRaw, ok := obj["start"]; ok {
		var start string
		_ = json.Unmarshal(startRaw, &start)
		endStr := "ongoing"
		if endRaw, ok := obj["end"]; ok {
			var end string
			if json.Unmarshal(endRaw, &end) == nil && end != "" {
				endStr = end
			}
		}
		return start + " – " + endStr
	}
	return ""
}

// ---- kanban ----

func renderKanban(b block) string {
	var body struct {
		Title   string `json:"title"`
		Columns []struct {
			Name  string `json:"name"`
			Cards []struct {
				Title string `json:"title"`
			} `json:"cards"`
		} `json:"columns"`
	}
	if err := decodeBody(b, "kanban", &body); err != nil {
		return ""
	}
	var out strings.Builder
	if t := strings.TrimSpace(body.Title); t != "" {
		out.WriteString("### ")
		out.WriteString(escapeMarkdown(t))
		out.WriteString("\n\n")
	}
	sections := make([]string, 0, len(body.Columns))
	for _, col := range body.Columns {
		var sec strings.Builder
		sec.WriteString("### ")
		sec.WriteString(escapeMarkdown(col.Name))
		for _, card := range col.Cards {
			sec.WriteString("\n- ")
			sec.WriteString(escapeMarkdown(card.Title))
		}
		sections = append(sections, sec.String())
	}
	out.WriteString(strings.Join(sections, "\n\n"))
	return strings.TrimRight(out.String(), "\n")
}

// ---- comparison_matrix ----

func renderComparisonMatrix(b block) string {
	var body struct {
		Title          string `json:"title"`
		RowLabelHeader string `json:"row_label_header"`
		Columns        []struct {
			Label string `json:"label"`
		} `json:"columns"`
		Rows []struct {
			Label string            `json:"label"`
			Cells []json.RawMessage `json:"cells"`
		} `json:"rows"`
	}
	if err := decodeBody(b, "comparison_matrix", &body); err != nil {
		return ""
	}
	var out strings.Builder
	if t := strings.TrimSpace(body.Title); t != "" {
		out.WriteString("### ")
		out.WriteString(escapeMarkdown(t))
		out.WriteString("\n\n")
	}

	width := len(body.Columns) + 1
	header := []string{cellEscape(body.RowLabelHeader)}
	for _, c := range body.Columns {
		header = append(header, cellEscape(c.Label))
	}
	writeRow(&out, header)
	out.WriteString("\n")
	writeSeparator(&out, width)
	for _, r := range body.Rows {
		row := []string{cellEscape(r.Label)}
		for i := 0; i < len(body.Columns); i++ {
			if i < len(r.Cells) {
				row = append(row, cellEscape(matrixCellRender(r.Cells[i])))
			} else {
				row = append(row, "")
			}
		}
		out.WriteString("\n")
		writeRow(&out, row)
	}
	return strings.TrimRight(out.String(), "\n")
}

// matrixCellRender renders one comparison_matrix cell. Four shapes per spec:
// glyph keyword (check/cross/dash), plain string, rich_text array, or
// object form {value: rich_text, accent?}.
func matrixCellRender(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		switch s {
		case "check":
			return "✓"
		case "cross":
			return "✗"
		case "dash":
			return "–"
		}
		return s
	}
	var arr []richText
	if err := json.Unmarshal(raw, &arr); err == nil {
		return renderRichText(arr)
	}
	var obj struct {
		Value []richText `json:"value"`
	}
	if err := json.Unmarshal(raw, &obj); err == nil {
		return renderRichText(obj.Value)
	}
	return ""
}

// ---- tabs ----

func renderTabs(b block) string {
	var body struct {
		Title  string `json:"title"`
		Panels []struct {
			Label  string  `json:"label"`
			Blocks []block `json:"blocks"`
		} `json:"panels"`
	}
	if err := decodeBody(b, "tabs", &body); err != nil {
		return ""
	}
	var out strings.Builder
	if t := strings.TrimSpace(body.Title); t != "" {
		out.WriteString("### ")
		out.WriteString(escapeMarkdown(t))
		out.WriteString("\n\n")
	}
	sections := make([]string, 0, len(body.Panels))
	for _, p := range body.Panels {
		head := "### " + escapeMarkdown(p.Label)
		inner := renderBlocks(p.Blocks)
		if inner == "" {
			sections = append(sections, head)
		} else {
			sections = append(sections, head+"\n\n"+inner)
		}
	}
	out.WriteString(strings.Join(sections, "\n\n"))
	return strings.TrimRight(out.String(), "\n")
}

// ---- columns ----

func renderColumns(b block) string {
	var body struct {
		Columns []struct {
			Blocks []block `json:"blocks"`
		} `json:"columns"`
	}
	if err := decodeBody(b, "columns", &body); err != nil {
		return ""
	}
	all := make([]block, 0)
	for _, c := range body.Columns {
		all = append(all, c.Blocks...)
	}
	return renderBlocks(all)
}

// ---- gallery ----

func renderGallery(b block) string {
	var body struct {
		Title  string `json:"title"`
		Images []struct {
			AssetID string     `json:"asset_id"`
			Alt     string     `json:"alt"`
			Caption []richText `json:"caption,omitempty"`
		} `json:"images"`
	}
	if err := decodeBody(b, "gallery", &body); err != nil {
		return ""
	}
	var out strings.Builder
	if t := strings.TrimSpace(body.Title); t != "" {
		out.WriteString("### ")
		out.WriteString(escapeMarkdown(t))
		out.WriteString("\n\n")
	}
	parts := make([]string, 0, len(body.Images))
	for _, img := range body.Images {
		if img.AssetID == "" {
			continue
		}
		alt := strings.TrimSpace(img.Alt)
		alt = strings.ReplaceAll(alt, "]", `\]`)
		url := miraAssetBase + img.AssetID
		section := "![" + alt + "](" + url + ")"
		if cap := strings.TrimSpace(renderRichText(img.Caption)); cap != "" {
			section += "\n\n" + cap
		}
		parts = append(parts, section)
	}
	out.WriteString(strings.Join(parts, "\n\n"))
	return strings.TrimRight(out.String(), "\n")
}

// ---- slides ----

func renderSlides(b block) string {
	var body struct {
		Title  string `json:"title"`
		Slides []struct {
			Title    string  `json:"title"`
			Subtitle string  `json:"subtitle,omitempty"`
			Blocks   []block `json:"blocks"`
		} `json:"slides"`
	}
	if err := decodeBody(b, "slides", &body); err != nil {
		return ""
	}
	var out strings.Builder
	if t := strings.TrimSpace(body.Title); t != "" {
		out.WriteString("### ")
		out.WriteString(escapeMarkdown(t))
		out.WriteString("\n\n")
	}
	sections := make([]string, 0, len(body.Slides))
	for _, s := range body.Slides {
		var sec strings.Builder
		sec.WriteString("### ")
		sec.WriteString(escapeMarkdown(s.Title))
		if sub := strings.TrimSpace(s.Subtitle); sub != "" {
			sec.WriteString("\n\n*")
			sec.WriteString(escapeMarkdown(sub))
			sec.WriteString("*")
		}
		inner := renderBlocks(s.Blocks)
		if inner != "" {
			sec.WriteString("\n\n")
			sec.WriteString(inner)
		}
		sections = append(sections, sec.String())
	}
	out.WriteString(strings.Join(sections, "\n\n"))
	return strings.TrimRight(out.String(), "\n")
}

// ---- calendar ----

func renderCalendar(b block) string {
	var body struct {
		Title  string `json:"title"`
		Month  string `json:"month"`
		Events []struct {
			Date  string `json:"date"`
			Title string `json:"title"`
		} `json:"events"`
	}
	if err := decodeBody(b, "calendar", &body); err != nil {
		return ""
	}
	var out strings.Builder
	if t := strings.TrimSpace(body.Title); t != "" {
		out.WriteString("### ")
		out.WriteString(escapeMarkdown(t))
		out.WriteString("\n\n")
	} else if m := strings.TrimSpace(body.Month); m != "" {
		out.WriteString("### ")
		out.WriteString(escapeMarkdown(m))
		out.WriteString("\n\n")
	}
	writeRow(&out, []string{"date", "event"})
	out.WriteString("\n")
	writeSeparator(&out, 2)
	for _, e := range body.Events {
		out.WriteString("\n")
		writeRow(&out, []string{cellEscape(e.Date), cellEscape(e.Title)})
	}
	return strings.TrimRight(out.String(), "\n")
}

// ---- map ----

func renderMap(b block) string {
	var body struct {
		Title   string `json:"title"`
		Markers []struct {
			Label string `json:"label"`
		} `json:"markers"`
	}
	if err := decodeBody(b, "map", &body); err != nil {
		return ""
	}
	descr := strings.TrimSpace(body.Title)
	if descr == "" && len(body.Markers) > 0 {
		descr = strings.TrimSpace(body.Markers[0].Label)
		if len(body.Markers) > 1 {
			descr += " (+" + strconv.Itoa(len(body.Markers)-1) + " more)"
		}
	}
	if descr == "" {
		descr = "(map)"
	}
	return "> 🗺️ Map: " + descr
}

// ---- video ----

func renderVideo(b block) string {
	var body struct {
		URL   string `json:"url"`
		Title string `json:"title"`
	}
	if err := decodeBody(b, "video", &body); err != nil {
		return ""
	}
	url := strings.TrimSpace(body.URL)
	if url == "" {
		return ""
	}
	title := strings.TrimSpace(body.Title)
	if title == "" {
		return "<" + url + ">"
	}
	return "[" + escapeMarkdown(title) + "](" + url + ")"
}

// ---- network ----

func renderNetwork(b block) string {
	var body struct {
		Title string `json:"title"`
	}
	_ = decodeBody(b, "network", &body)
	if t := strings.TrimSpace(body.Title); t != "" {
		return "> 🕸️ Network diagram (mira-only): " + t
	}
	return "> 🕸️ Network diagram (mira-only)"
}

// ---- diff ----

func renderDiff(b block) string {
	var body struct {
		Title string `json:"title"`
		Diff  string `json:"diff"`
	}
	if err := decodeBody(b, "diff", &body); err != nil {
		return ""
	}
	var out strings.Builder
	if t := strings.TrimSpace(body.Title); t != "" {
		out.WriteString("### ")
		out.WriteString(escapeMarkdown(t))
		out.WriteString("\n\n")
	}
	src := strings.TrimRight(body.Diff, "\n")
	out.WriteString("```diff\n")
	out.WriteString(src)
	out.WriteString("\n```")
	return out.String()
}

// ---- choice ----

func renderChoice(b block) string {
	var body struct {
		Prompt   string   `json:"prompt"`
		Multi    bool     `json:"multi"`
		Selected []string `json:"selected"`
		Options  []struct {
			ID    string `json:"id"`
			Label string `json:"label"`
		} `json:"options"`
	}
	if err := decodeBody(b, "choice", &body); err != nil {
		return ""
	}
	selectedSet := make(map[string]struct{}, len(body.Selected))
	for _, id := range body.Selected {
		selectedSet[id] = struct{}{}
	}
	var out strings.Builder
	if p := strings.TrimSpace(body.Prompt); p != "" {
		out.WriteString("**")
		out.WriteString(escapeMarkdown(p))
		out.WriteString("**\n\n")
	}
	for _, opt := range body.Options {
		mark := "[ ]"
		if _, ok := selectedSet[opt.ID]; ok {
			mark = "[x]"
		}
		out.WriteString("- ")
		out.WriteString(mark)
		out.WriteString(" ")
		out.WriteString(escapeMarkdown(opt.Label))
		out.WriteString("\n")
	}
	return strings.TrimRight(out.String(), "\n")
}

// ---- approve ----

func renderApprove(b block) string {
	var body struct {
		Prompt   string `json:"prompt"`
		Approved bool   `json:"approved"`
	}
	if err := decodeBody(b, "approve", &body); err != nil {
		return ""
	}
	mark := "[ ]"
	status := "Pending"
	if body.Approved {
		mark = "[x]"
		status = "Approved"
	}
	var out strings.Builder
	if p := strings.TrimSpace(body.Prompt); p != "" {
		out.WriteString("**")
		out.WriteString(escapeMarkdown(p))
		out.WriteString("**\n\n")
	}
	out.WriteString("- ")
	out.WriteString(mark)
	out.WriteString(" ")
	out.WriteString(status)
	return out.String()
}

// ---- shared helpers ----

// cellEscape sanitizes a string for use inside a GFM table cell: pipes are
// escaped, newlines collapsed to spaces. Empty string is preserved (callers
// handle the blank-cell space-padding via writeRow).
func cellEscape(s string) string {
	s = strings.ReplaceAll(s, "|", `\|`)
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}

// stringifyJSON renders a json.RawMessage as a compact display string:
// numbers become Go-formatted floats, strings are unwrapped, arrays/objects
// fall back to verbatim JSON. Used for chart series data and stat_grid tile
// values, both of which accept mixed scalar/array shapes.
func stringifyJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var v interface{}
	if err := json.Unmarshal(raw, &v); err != nil {
		return string(raw)
	}
	return formatJSONValue(v)
}

func formatJSONValue(v interface{}) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	case []interface{}:
		parts := make([]string, 0, len(x))
		for _, item := range x {
			parts = append(parts, formatJSONValue(item))
		}
		return "[" + strings.Join(parts, ", ") + "]"
	default:
		// Object or unknown — fall back to compact JSON.
		out, err := json.Marshal(v)
		if err != nil {
			return ""
		}
		return string(out)
	}
}
