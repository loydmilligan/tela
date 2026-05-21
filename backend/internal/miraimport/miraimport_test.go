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
	_, body := mustConvert(t, `{
		"template":"page",
		"blocks":[
			{"type":"some_future_thing","some_future_thing":{}}
		]
	}`)
	want := "> ⚠️ Unsupported mira block: some_future_thing"
	if body != want {
		t.Fatalf("body = %q, want %q", body, want)
	}
}

func TestConvert_Tier2_BlocksFallToStub(t *testing.T) {
	// A.2 will route these to real Tier-2 renderers. For A.1 they must hit
	// the unknown-block stub so import still succeeds.
	for _, tier2Type := range []string{"chart", "kanban", "timeline", "stat_grid", "diff"} {
		t.Run(tier2Type, func(t *testing.T) {
			payload := `{"template":"page","blocks":[{"type":"` + tier2Type + `","` + tier2Type + `":{}}]}`
			_, body := mustConvert(t, payload)
			if !strings.Contains(body, "Unsupported mira block: "+tier2Type) {
				t.Fatalf("body = %q, want stub for %s", body, tier2Type)
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
