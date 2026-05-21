package miraimport

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Each test feeds an inline JSON literal through Convert and checks the
// title + body output. The literals are deliberately minimal — they exercise
// one block type each, not the full mira schema. The broad smoke test at the
// bottom reads testdata/showcase.json once to verify Convert tolerates a
// real-world payload.

func mustConvert(t *testing.T, payload string) (string, string) {
	t.Helper()
	title, body, err := Convert([]byte(payload))
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	return title, body
}

func TestConvert_EmptyBlocks_ReturnsFallbackTitle(t *testing.T) {
	title, body, err := Convert([]byte(`{"template":"page","blocks":[]}`))
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if title != fallbackTitle {
		t.Fatalf("title = %q, want %q", title, fallbackTitle)
	}
	if body != "" {
		t.Fatalf("body = %q, want empty", body)
	}
}

func TestConvert_MalformedJSON_ReturnsError(t *testing.T) {
	_, _, err := Convert([]byte(`not json {{{`))
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
}

func TestConvert_NoHeading1_FallsBackToTitleConstant(t *testing.T) {
	title, body := mustConvert(t, `{
		"template":"page",
		"blocks":[
			{"type":"paragraph","paragraph":{"rich_text":[{"type":"text","text":{"content":"just a paragraph"}}]}}
		]
	}`)
	if title != fallbackTitle {
		t.Fatalf("title = %q, want %q", title, fallbackTitle)
	}
	if body != "just a paragraph" {
		t.Fatalf("body = %q", body)
	}
}

func TestConvert_TitleFromFirstHeading1(t *testing.T) {
	title, _ := mustConvert(t, `{
		"template":"page",
		"blocks":[
			{"type":"heading_1","heading_1":{"rich_text":[{"type":"text","text":{"content":"Hello "}},{"type":"text","text":{"content":"World"}}]}}
		]
	}`)
	if title != "Hello World" {
		t.Fatalf("title = %q, want %q", title, "Hello World")
	}
}

func TestConvert_Headings(t *testing.T) {
	_, body := mustConvert(t, `{
		"template":"page",
		"blocks":[
			{"type":"heading_1","heading_1":{"rich_text":[{"type":"text","text":{"content":"H1"}}]}},
			{"type":"heading_2","heading_2":{"rich_text":[{"type":"text","text":{"content":"H2"}}]}},
			{"type":"heading_3","heading_3":{"rich_text":[{"type":"text","text":{"content":"H3"}}]}}
		]
	}`)
	want := "# H1\n\n## H2\n\n### H3"
	if body != want {
		t.Fatalf("body = %q, want %q", body, want)
	}
}

func TestConvert_Paragraph_WithMarks(t *testing.T) {
	_, body := mustConvert(t, `{
		"template":"page",
		"blocks":[
			{"type":"paragraph","paragraph":{"rich_text":[
				{"type":"text","text":{"content":"plain "}},
				{"type":"text","text":{"content":"bold"},"annotations":{"bold":true}},
				{"type":"text","text":{"content":" "}},
				{"type":"text","text":{"content":"italic"},"annotations":{"italic":true}},
				{"type":"text","text":{"content":" "}},
				{"type":"text","text":{"content":"code"},"annotations":{"code":true}},
				{"type":"text","text":{"content":" "}},
				{"type":"text","text":{"content":"strike"},"annotations":{"strikethrough":true}}
			]}}
		]
	}`)
	want := "plain **bold** *italic* `code` ~~strike~~"
	if body != want {
		t.Fatalf("body = %q, want %q", body, want)
	}
}

func TestConvert_Paragraph_StackedMarks(t *testing.T) {
	// bold+italic+strike → nested outer-to-inner: **...*~~...~~*...**
	_, body := mustConvert(t, `{
		"template":"page",
		"blocks":[
			{"type":"paragraph","paragraph":{"rich_text":[
				{"type":"text","text":{"content":"x"},"annotations":{"bold":true,"italic":true,"strikethrough":true}}
			]}}
		]
	}`)
	want := "***~~x~~***"
	if body != want {
		t.Fatalf("body = %q, want %q", body, want)
	}
}

func TestConvert_Paragraph_CodeAnnotation_NoNestedMarks(t *testing.T) {
	// code annotation is literal — other marks are dropped, content is verbatim.
	_, body := mustConvert(t, `{
		"template":"page",
		"blocks":[
			{"type":"paragraph","paragraph":{"rich_text":[
				{"type":"text","text":{"content":"*not bold*"},"annotations":{"bold":true,"code":true}}
			]}}
		]
	}`)
	want := "`*not bold*`"
	if body != want {
		t.Fatalf("body = %q, want %q", body, want)
	}
}

func TestConvert_Paragraph_Link_Escaped(t *testing.T) {
	_, body := mustConvert(t, `{
		"template":"page",
		"blocks":[
			{"type":"paragraph","paragraph":{"rich_text":[
				{"type":"text","text":{"content":"a link","link":{"url":"https://ex.com"}}}
			]}}
		]
	}`)
	want := "[a link](https://ex.com)"
	if body != want {
		t.Fatalf("body = %q, want %q", body, want)
	}
}

func TestConvert_Paragraph_HrefFallback(t *testing.T) {
	// mira spec: when both `text.link.url` and top-level `href` are present,
	// `text.link.url` wins. When only `href` is present it's the link source.
	_, body := mustConvert(t, `{
		"template":"page",
		"blocks":[
			{"type":"paragraph","paragraph":{"rich_text":[
				{"type":"text","text":{"content":"href only"},"href":"https://h.com"}
			]}}
		]
	}`)
	want := "[href only](https://h.com)"
	if body != want {
		t.Fatalf("body = %q, want %q", body, want)
	}
}

func TestConvert_Paragraph_EscapesSpecials(t *testing.T) {
	_, body := mustConvert(t, `{
		"template":"page",
		"blocks":[
			{"type":"paragraph","paragraph":{"rich_text":[
				{"type":"text","text":{"content":"asterisk * underscore _ bracket [x]"}}
			]}}
		]
	}`)
	want := `asterisk \* underscore \_ bracket \[x\]`
	if body != want {
		t.Fatalf("body = %q, want %q", body, want)
	}
}

func TestConvert_BulletedList(t *testing.T) {
	_, body := mustConvert(t, `{
		"template":"page",
		"blocks":[
			{"type":"bulleted_list_item","bulleted_list_item":{"rich_text":[{"type":"text","text":{"content":"one"}}]}},
			{"type":"bulleted_list_item","bulleted_list_item":{"rich_text":[{"type":"text","text":{"content":"two"}}]}}
		]
	}`)
	want := "- one\n\n- two"
	if body != want {
		t.Fatalf("body = %q, want %q", body, want)
	}
}

func TestConvert_BulletedList_Nested(t *testing.T) {
	_, body := mustConvert(t, `{
		"template":"page",
		"blocks":[
			{"type":"bulleted_list_item","bulleted_list_item":{
				"rich_text":[{"type":"text","text":{"content":"parent"}}],
				"children":[
					{"type":"bulleted_list_item","bulleted_list_item":{"rich_text":[{"type":"text","text":{"content":"child"}}]}}
				]
			}}
		]
	}`)
	want := "- parent\n  - child"
	if body != want {
		t.Fatalf("body = %q, want %q", body, want)
	}
}

func TestConvert_NumberedList(t *testing.T) {
	_, body := mustConvert(t, `{
		"template":"page",
		"blocks":[
			{"type":"numbered_list_item","numbered_list_item":{"rich_text":[{"type":"text","text":{"content":"first"}}]}},
			{"type":"numbered_list_item","numbered_list_item":{"rich_text":[{"type":"text","text":{"content":"second"}}]}}
		]
	}`)
	want := "1. first\n\n1. second"
	if body != want {
		t.Fatalf("body = %q, want %q", body, want)
	}
}

func TestConvert_Code(t *testing.T) {
	_, body := mustConvert(t, `{
		"template":"page",
		"blocks":[
			{"type":"code","code":{"language":"go","rich_text":[{"type":"text","text":{"content":"package main\nfunc main(){}\n"}}]}}
		]
	}`)
	want := "```go\npackage main\nfunc main(){}\n```"
	if body != want {
		t.Fatalf("body = %q, want %q", body, want)
	}
}

func TestConvert_Code_PlainTextLang_Stripped(t *testing.T) {
	_, body := mustConvert(t, `{
		"template":"page",
		"blocks":[
			{"type":"code","code":{"language":"plain text","rich_text":[{"type":"text","text":{"content":"hello"}}]}}
		]
	}`)
	want := "```\nhello\n```"
	if body != want {
		t.Fatalf("body = %q, want %q", body, want)
	}
}

func TestConvert_Quote(t *testing.T) {
	_, body := mustConvert(t, `{
		"template":"page",
		"blocks":[
			{"type":"quote","quote":{"rich_text":[{"type":"text","text":{"content":"the quote"}}]}}
		]
	}`)
	want := "> the quote"
	if body != want {
		t.Fatalf("body = %q, want %q", body, want)
	}
}

func TestConvert_Divider(t *testing.T) {
	_, body := mustConvert(t, `{
		"template":"page",
		"blocks":[{"type":"divider","divider":{}}]
	}`)
	if body != "---" {
		t.Fatalf("body = %q", body)
	}
}

func TestConvert_Image_External(t *testing.T) {
	_, body := mustConvert(t, `{
		"template":"page",
		"blocks":[
			{"type":"image","image":{"type":"external","external":{"url":"https://ex.com/i.png"},"caption":[{"type":"text","text":{"content":"a cat"}}]}}
		]
	}`)
	want := "![a cat](https://ex.com/i.png)\n\na cat"
	if body != want {
		t.Fatalf("body = %q, want %q", body, want)
	}
}

func TestConvert_Image_File(t *testing.T) {
	_, body := mustConvert(t, `{
		"template":"page",
		"blocks":[
			{"type":"image","image":{"type":"file","file":{"url":"https://ex.com/i.png"}}}
		]
	}`)
	want := "![](https://ex.com/i.png)"
	if body != want {
		t.Fatalf("body = %q, want %q", body, want)
	}
}

func TestConvert_Table_WithColumnHeader(t *testing.T) {
	_, body := mustConvert(t, `{
		"template":"page",
		"blocks":[
			{"type":"table","table":{
				"table_width":2,
				"has_column_header":true,
				"children":[
					{"type":"table_row","table_row":{"cells":[
						[{"type":"text","text":{"content":"name"}}],
						[{"type":"text","text":{"content":"value"}}]
					]}},
					{"type":"table_row","table_row":{"cells":[
						[{"type":"text","text":{"content":"foo"}}],
						[{"type":"text","text":{"content":"42"}}]
					]}}
				]
			}}
		]
	}`)
	want := "| name | value |\n| --- | --- |\n| foo | 42 |"
	if body != want {
		t.Fatalf("body = %q, want %q", body, want)
	}
}

func TestConvert_Callout_VariantFromEmoji(t *testing.T) {
	cases := []struct {
		emoji   string
		variant string
	}{
		{"ℹ️", "NOTE"},
		{"💡", "TIP"},
		{"❗", "IMPORTANT"},
		{"⚠️", "WARNING"},
		{"🛑", "CAUTION"},
		{"🤷", "NOTE"}, // unknown emoji → NOTE fallback
	}
	for _, c := range cases {
		t.Run(c.variant+"_"+c.emoji, func(t *testing.T) {
			payload := `{
				"template":"page",
				"blocks":[
					{"type":"callout","callout":{
						"icon":{"type":"emoji","emoji":"` + c.emoji + `"},
						"rich_text":[{"type":"text","text":{"content":"body"}}]
					}}
				]
			}`
			_, body := mustConvert(t, payload)
			want := "> [!" + c.variant + "]\n> body"
			if body != want {
				t.Fatalf("body = %q, want %q", body, want)
			}
		})
	}
}

func TestConvert_Toggle(t *testing.T) {
	_, body := mustConvert(t, `{
		"template":"page",
		"blocks":[
			{"type":"toggle","toggle":{
				"rich_text":[{"type":"text","text":{"content":"Click me"}}],
				"children":[
					{"type":"paragraph","paragraph":{"rich_text":[{"type":"text","text":{"content":"hidden"}}]}}
				]
			}}
		]
	}`)
	want := "<details><summary>Click me</summary>\n\nhidden\n\n</details>"
	if body != want {
		t.Fatalf("body = %q, want %q", body, want)
	}
}

func TestConvert_Mermaid(t *testing.T) {
	_, body := mustConvert(t, `{
		"template":"page",
		"blocks":[
			{"type":"mermaid","mermaid":{"source":"flowchart TD\n  A --> B"}}
		]
	}`)
	want := "```mermaid\nflowchart TD\n  A --> B\n```"
	if body != want {
		t.Fatalf("body = %q, want %q", body, want)
	}
}

func TestConvert_UnknownBlock_Stub(t *testing.T) {
	// Forward-compat: a made-up future block type must hit the unknown-block
	// stub without a Tier-2 footer (the stub is its own marker).
	_, body := mustConvert(t, `{
		"template":"page",
		"blocks":[
			{"type":"__future_block__","__future_block__":{}}
		]
	}`)
	want := "> ⚠️ Unsupported mira block: __future_block__"
	if body != want {
		t.Fatalf("body = %q, want %q", body, want)
	}
	if strings.Contains(body, "visual fidelity reduced") {
		t.Fatalf("unknown stub should not carry Tier-2 footer; body = %q", body)
	}
}

// ----- Tier-2 placeholder converters -----
//
// Each Tier-2 test exercises the renderer with a minimal inline JSON literal
// and asserts both the structural output AND that the Tier-2 footer is
// appended.

const tier2FooterLit = "> _Imported from mira render — visual fidelity reduced._"

func wantTier2(content string) string {
	return content + "\n\n" + tier2FooterLit
}

func TestConvert_Tier2_Chart(t *testing.T) {
	_, body := mustConvert(t, `{
		"template":"page",
		"blocks":[
			{"type":"chart","chart":{
				"chart_type":"line",
				"title":"Weekly renders",
				"x_axis":{"type":"category","label":"week","categories":["W1","W2"]},
				"y_axis":{"type":"number","label":"renders"},
				"series":[
					{"name":"Free","data":[10,20]},
					{"name":"Paid","data":[5,15]}
				]
			}}
		]
	}`)
	want := wantTier2("### Weekly renders\n\n| week | Free | Paid |\n| --- | --- | --- |\n| W1 | 10 | 5 |\n| W2 | 20 | 15 |")
	if body != want {
		t.Fatalf("body = %q\nwant = %q", body, want)
	}
}

func TestConvert_Tier2_Chart_NoCategories_BulletFallback(t *testing.T) {
	// Pie/donut have no x_axis — fall back to a per-series bullet list.
	_, body := mustConvert(t, `{
		"template":"page",
		"blocks":[
			{"type":"chart","chart":{
				"chart_type":"pie",
				"title":"Browser share",
				"series":[{"name":"Chrome","data":[60,30,10]}]
			}}
		]
	}`)
	want := wantTier2("### Browser share\n\n- **Chrome:** 60, 30, 10")
	if body != want {
		t.Fatalf("body = %q\nwant = %q", body, want)
	}
}

func TestConvert_Tier2_StatGrid(t *testing.T) {
	_, body := mustConvert(t, `{
		"template":"page",
		"blocks":[
			{"type":"stat_grid","stat_grid":{
				"title":"KPIs",
				"tiles":[
					{"label":"Revenue","value":"$4.2M"},
					{"label":"Latency","value":86,"unit":"ms"}
				]
			}}
		]
	}`)
	want := wantTier2("### KPIs\n\n- **Revenue:** $4.2M\n- **Latency:** 86 ms")
	if body != want {
		t.Fatalf("body = %q\nwant = %q", body, want)
	}
}

func TestConvert_Tier2_Timeline(t *testing.T) {
	_, body := mustConvert(t, `{
		"template":"page",
		"blocks":[
			{"type":"timeline","timeline":{
				"title":"Releases",
				"events":[
					{"date":"2026-01-01","label":"v1 ships"},
					{"date":{"sort":"2026-04-01","display":"Q2 2026"},"label":"v2 plan"}
				]
			}}
		]
	}`)
	want := wantTier2("### Releases\n\n### 2026-01-01\n- v1 ships\n\n### Q2 2026\n- v2 plan")
	if body != want {
		t.Fatalf("body = %q\nwant = %q", body, want)
	}
}

func TestConvert_Tier2_Kanban(t *testing.T) {
	_, body := mustConvert(t, `{
		"template":"page",
		"blocks":[
			{"type":"kanban","kanban":{
				"title":"Sprint 24",
				"columns":[
					{"name":"Todo","cards":[{"title":"task one"},{"title":"task two"}]},
					{"name":"Done","cards":[{"title":"task three"}]}
				]
			}}
		]
	}`)
	want := wantTier2("### Sprint 24\n\n### Todo\n- task one\n- task two\n\n### Done\n- task three")
	if body != want {
		t.Fatalf("body = %q\nwant = %q", body, want)
	}
}

func TestConvert_Tier2_ComparisonMatrix(t *testing.T) {
	_, body := mustConvert(t, `{
		"template":"page",
		"blocks":[
			{"type":"comparison_matrix","comparison_matrix":{
				"row_label_header":"Feature",
				"columns":[{"label":"Free"},{"label":"Pro"}],
				"rows":[
					{"label":"SSO","cells":["cross","check"]},
					{"label":"Storage","cells":["5 GB","100 GB"]}
				]
			}}
		]
	}`)
	want := wantTier2("| Feature | Free | Pro |\n| --- | --- | --- |\n| SSO | ✗ | ✓ |\n| Storage | 5 GB | 100 GB |")
	if body != want {
		t.Fatalf("body = %q\nwant = %q", body, want)
	}
}

func TestConvert_Tier2_Tabs_FlattenToSections(t *testing.T) {
	_, body := mustConvert(t, `{
		"template":"page",
		"blocks":[
			{"type":"tabs","tabs":{
				"panels":[
					{"label":"Overview","blocks":[
						{"type":"paragraph","paragraph":{"rich_text":[{"type":"text","text":{"content":"intro text"}}]}}
					]},
					{"label":"Pricing","blocks":[
						{"type":"paragraph","paragraph":{"rich_text":[{"type":"text","text":{"content":"$29/mo"}}]}}
					]}
				]
			}}
		]
	}`)
	want := wantTier2("### Overview\n\nintro text\n\n### Pricing\n\n$29/mo")
	if body != want {
		t.Fatalf("body = %q\nwant = %q", body, want)
	}
}

func TestConvert_Tier2_Columns_Flatten(t *testing.T) {
	_, body := mustConvert(t, `{
		"template":"page",
		"blocks":[
			{"type":"columns","columns":{
				"columns":[
					{"blocks":[
						{"type":"heading_3","heading_3":{"rich_text":[{"type":"text","text":{"content":"Free"}}]}}
					]},
					{"blocks":[
						{"type":"heading_3","heading_3":{"rich_text":[{"type":"text","text":{"content":"Paid"}}]}}
					]}
				]
			}}
		]
	}`)
	want := wantTier2("### Free\n\n### Paid")
	if body != want {
		t.Fatalf("body = %q\nwant = %q", body, want)
	}
}

func TestConvert_Tier2_Gallery(t *testing.T) {
	_, body := mustConvert(t, `{
		"template":"page",
		"blocks":[
			{"type":"gallery","gallery":{
				"title":"Tiles",
				"images":[
					{"asset_id":"abc123","alt":"first","caption":[{"type":"text","text":{"content":"Tile 1"}}]},
					{"asset_id":"def456","alt":"second"}
				]
			}}
		]
	}`)
	want := wantTier2("### Tiles\n\n![first](https://mira.cagdas.io/asset/abc123)\n\nTile 1\n\n![second](https://mira.cagdas.io/asset/def456)")
	if body != want {
		t.Fatalf("body = %q\nwant = %q", body, want)
	}
}

func TestConvert_Tier2_Slides(t *testing.T) {
	_, body := mustConvert(t, `{
		"template":"page",
		"blocks":[
			{"type":"slides","slides":{
				"slides":[
					{"title":"Cover","subtitle":"Q3 review","blocks":[
						{"type":"paragraph","paragraph":{"rich_text":[{"type":"text","text":{"content":"first slide body"}}]}}
					]},
					{"title":"Ask","blocks":[
						{"type":"paragraph","paragraph":{"rich_text":[{"type":"text","text":{"content":"approve $4M"}}]}}
					]}
				]
			}}
		]
	}`)
	want := wantTier2("### Cover\n\n*Q3 review*\n\nfirst slide body\n\n### Ask\n\napprove $4M")
	if body != want {
		t.Fatalf("body = %q\nwant = %q", body, want)
	}
}

func TestConvert_Tier2_Calendar(t *testing.T) {
	_, body := mustConvert(t, `{
		"template":"page",
		"blocks":[
			{"type":"calendar","calendar":{
				"title":"May launches",
				"month":"2026-05",
				"events":[
					{"date":"2026-05-04","title":"Spec freeze"},
					{"date":"2026-05-28","title":"GA launch"}
				]
			}}
		]
	}`)
	want := wantTier2("### May launches\n\n| date | event |\n| --- | --- |\n| 2026-05-04 | Spec freeze |\n| 2026-05-28 | GA launch |")
	if body != want {
		t.Fatalf("body = %q\nwant = %q", body, want)
	}
}

func TestConvert_Tier2_Map(t *testing.T) {
	_, body := mustConvert(t, `{
		"template":"page",
		"blocks":[
			{"type":"map","map":{
				"title":"Offices",
				"markers":[{"lat":37.7,"lng":-122.4,"label":"SF"}]
			}}
		]
	}`)
	want := wantTier2("> 🗺️ Map: Offices")
	if body != want {
		t.Fatalf("body = %q\nwant = %q", body, want)
	}
}

func TestConvert_Tier2_Map_NoTitle_UsesFirstMarker(t *testing.T) {
	_, body := mustConvert(t, `{
		"template":"page",
		"blocks":[
			{"type":"map","map":{
				"markers":[
					{"lat":37.7,"lng":-122.4,"label":"SF"},
					{"lat":35.6,"lng":139.6,"label":"Tokyo"}
				]
			}}
		]
	}`)
	want := wantTier2("> 🗺️ Map: SF (+1 more)")
	if body != want {
		t.Fatalf("body = %q\nwant = %q", body, want)
	}
}

func TestConvert_Tier2_Video_WithTitle(t *testing.T) {
	_, body := mustConvert(t, `{
		"template":"page",
		"blocks":[
			{"type":"video","video":{"url":"https://youtu.be/abc","title":"My talk"}}
		]
	}`)
	want := wantTier2("[My talk](https://youtu.be/abc)")
	if body != want {
		t.Fatalf("body = %q\nwant = %q", body, want)
	}
}

func TestConvert_Tier2_Video_NoTitle_AutoLink(t *testing.T) {
	_, body := mustConvert(t, `{
		"template":"page",
		"blocks":[
			{"type":"video","video":{"url":"https://youtu.be/xyz"}}
		]
	}`)
	want := wantTier2("<https://youtu.be/xyz>")
	if body != want {
		t.Fatalf("body = %q\nwant = %q", body, want)
	}
}

func TestConvert_Tier2_Network(t *testing.T) {
	_, body := mustConvert(t, `{
		"template":"page",
		"blocks":[
			{"type":"network","network":{"topokit_config":{"nodes":[]}}}
		]
	}`)
	want := wantTier2("> 🕸️ Network diagram (mira-only)")
	if body != want {
		t.Fatalf("body = %q\nwant = %q", body, want)
	}
}

func TestConvert_Tier2_Diff(t *testing.T) {
	_, body := mustConvert(t, `{
		"template":"page",
		"blocks":[
			{"type":"diff","diff":{
				"title":"auth fix",
				"diff":"--- a/x\n+++ b/x\n-old line\n+new line\n"
			}}
		]
	}`)
	want := wantTier2("### auth fix\n\n" + "```" + "diff\n--- a/x\n+++ b/x\n-old line\n+new line\n" + "```")
	if body != want {
		t.Fatalf("body = %q\nwant = %q", body, want)
	}
}

func TestConvert_Tier2_Choice(t *testing.T) {
	_, body := mustConvert(t, `{
		"template":"page",
		"blocks":[
			{"type":"choice","choice":{
				"prompt":"Sprint checklist",
				"multi":true,
				"options":[
					{"id":"docs","label":"Update docs"},
					{"id":"tests","label":"Write tests"},
					{"id":"deploy","label":"Deploy to prod"}
				],
				"selected":["docs","tests"]
			}}
		]
	}`)
	want := wantTier2("**Sprint checklist**\n\n- [x] Update docs\n- [x] Write tests\n- [ ] Deploy to prod")
	if body != want {
		t.Fatalf("body = %q\nwant = %q", body, want)
	}
}

func TestConvert_Tier2_Approve(t *testing.T) {
	_, body := mustConvert(t, `{
		"template":"page",
		"blocks":[
			{"type":"approve","approve":{"prompt":"Sign off?","approved":true}}
		]
	}`)
	want := wantTier2("**Sign off?**\n\n- [x] Approved")
	if body != want {
		t.Fatalf("body = %q\nwant = %q", body, want)
	}
}

func TestConvert_Tier2_FooterPresentOnAllTypes(t *testing.T) {
	// Each Tier-2 type's minimum payload should render with the footer line.
	// Maps to the appendTier2Footer helper's invariant.
	cases := []struct {
		name    string
		payload string
	}{
		{"chart", `{"type":"chart","chart":{"chart_type":"line","title":"t","x_axis":{"type":"category","categories":["a"]},"y_axis":{"type":"number"},"series":[{"name":"s","data":[1]}]}}`},
		{"stat_grid", `{"type":"stat_grid","stat_grid":{"tiles":[{"label":"a","value":1},{"label":"b","value":2}]}}`},
		{"timeline", `{"type":"timeline","timeline":{"events":[{"date":"2026-01-01","label":"x"}]}}`},
		{"kanban", `{"type":"kanban","kanban":{"columns":[{"name":"c","cards":[{"title":"t"}]}]}}`},
		{"comparison_matrix", `{"type":"comparison_matrix","comparison_matrix":{"columns":[{"label":"a"},{"label":"b"}],"rows":[{"label":"r","cells":["check","cross"]}]}}`},
		{"tabs", `{"type":"tabs","tabs":{"panels":[{"label":"x","blocks":[{"type":"paragraph","paragraph":{"rich_text":[{"type":"text","text":{"content":"y"}}]}}]}]}}`},
		{"columns", `{"type":"columns","columns":{"columns":[{"blocks":[{"type":"paragraph","paragraph":{"rich_text":[{"type":"text","text":{"content":"a"}}]}}]}]}}`},
		{"gallery", `{"type":"gallery","gallery":{"images":[{"asset_id":"id","alt":"alt"}]}}`},
		{"slides", `{"type":"slides","slides":{"slides":[{"title":"t","blocks":[]}]}}`},
		{"calendar", `{"type":"calendar","calendar":{"month":"2026-05","events":[{"date":"2026-05-01","title":"x"}]}}`},
		{"map", `{"type":"map","map":{"markers":[{"lat":0,"lng":0,"label":"x"}]}}`},
		{"video", `{"type":"video","video":{"url":"https://youtu.be/x"}}`},
		{"network", `{"type":"network","network":{"topokit_config":{}}}`},
		{"diff", `{"type":"diff","diff":{"diff":"--- a\n+++ b\n"}}`},
		{"choice", `{"type":"choice","choice":{"prompt":"p","options":[{"id":"a","label":"A"},{"id":"b","label":"B"}]}}`},
		{"approve", `{"type":"approve","approve":{"prompt":"p"}}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			payload := `{"template":"page","blocks":[` + c.payload + `]}`
			_, body := mustConvert(t, payload)
			if !strings.HasSuffix(body, "\n\n"+tier2FooterLit) {
				t.Fatalf("%s body missing Tier-2 footer; body = %q", c.name, body)
			}
		})
	}
}

// Broad smoke test against a real mira render. testdata/showcase.json was
// snapshotted from https://mira.cagdas.io/p/showcase.json. The assertion is
// deliberately loose: Convert must succeed and produce a non-empty title +
// body. Per-block correctness is covered by the focused tests above.
func TestConvert_ShowcaseSmoke(t *testing.T) {
	path := filepath.Join("testdata", "showcase.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("read %s: %v", path, err)
	}
	title, body, err := Convert(data)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if title == "" {
		t.Fatalf("title is empty")
	}
	if body == "" {
		t.Fatalf("body is empty")
	}
	// Heading-1 source content "Mira Showcase" should appear in title.
	if !strings.Contains(title, "Mira") {
		t.Fatalf("title %q missing 'Mira'", title)
	}
}
