package api

import (
	"context"
	_ "embed"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Interactive widget bundles (MCP Apps, the 2026-01 Apps-SDK ⊕ MCP-UI standard).
// A tool that links one of these renders its structuredContent in a sandboxed
// iframe instead of plain text. Each widget is registered as TWO ui:// resources
// with the same HTML body but different MIME types: ChatGPT (Apps SDK) reads the
// text/html+skybridge variant via _meta["openai/outputTemplate"]; Claude / other
// MCP-Apps hosts read the text/html;profile=mcp-app variant via
// _meta.ui.resourceUri. The HTML feature-detects window.openai vs the MCP-Apps
// bridge, so one bundle serves both.

//go:embed widgets/page_reader.html
var widgetPageReaderHTML string

//go:embed widgets/search_results.html
var widgetSearchResultsHTML string

// widgetBridgeJS is the shared, dependency-free host bridge (ChatGPT Apps SDK +
// MCP Apps postMessage). It's injected into each widget at the <!--TELA_BRIDGE-->
// marker so there's no runtime import (Claude's sandbox blocks external fetches).
//
//go:embed widgets/bridge.js
var widgetBridgeJS string

const widgetBridgeMarker = "<!--TELA_BRIDGE-->"

// injectBridge replaces the bridge marker with an inline <script> carrying the
// shared bridge source, so the served HTML is fully self-contained.
func injectBridge(html string) string {
	return strings.Replace(html, widgetBridgeMarker, "<script>"+widgetBridgeJS+"</script>", 1)
}

const (
	mimeWidgetOpenAI = "text/html+skybridge"
	mimeWidgetMCPApp = "text/html;profile=mcp-app"

	uiPageReaderOpenAI = "ui://tela/page-reader/openai"
	uiPageReaderMCPApp = "ui://tela/page-reader/mcp"
	uiSearchOpenAI     = "ui://tela/search-results/openai"
	uiSearchMCPApp     = "ui://tela/search-results/mcp"
)

// registerMCPWidgets registers the widget bundles as ui:// resources.
func (s *Server) registerMCPWidgets(server *mcp.Server) {
	csp := s.widgetResourceMeta()
	reg := func(uri, mime, body string) {
		server.AddResource(&mcp.Resource{
			URI:      uri,
			Name:     "tela widget",
			MIMEType: mime,
			Meta:     csp,
		}, func(_ context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{
				{URI: req.Params.URI, MIMEType: mime, Text: body},
			}}, nil
		})
	}
	pageHTML := injectBridge(widgetPageReaderHTML)
	searchHTML := injectBridge(widgetSearchResultsHTML)
	reg(uiPageReaderOpenAI, mimeWidgetOpenAI, pageHTML)
	reg(uiPageReaderMCPApp, mimeWidgetMCPApp, pageHTML)
	reg(uiSearchOpenAI, mimeWidgetOpenAI, searchHTML)
	reg(uiSearchMCPApp, mimeWidgetMCPApp, searchHTML)
}

// widgetResourceMeta is the OpenAI widget CSP. The bridge is inlined (no external
// imports), so the iframe only needs to reach the tela origin — derived from
// publicBaseURL so self-hosters get their own.
func (s *Server) widgetResourceMeta() mcp.Meta {
	base := publicBaseURL()
	resourceDomains := []string{}
	connectDomains := []string{}
	if base != "" {
		resourceDomains = append(resourceDomains, base)
		connectDomains = append(connectDomains, base)
	}
	return mcp.Meta{
		"openai/widgetCSP": map[string]any{
			"connect_domains":  connectDomains,
			"resource_domains": resourceDomains,
		},
	}
}

// widgetToolMeta is the _meta to attach to a tool whose output renders in a
// widget: the ChatGPT outputTemplate + the MCP-Apps resourceUri + display hints.
func widgetToolMeta(openaiURI, mcpURI, description, invoking, invoked string) mcp.Meta {
	return mcp.Meta{
		"openai/outputTemplate":          openaiURI,
		"ui":                             map[string]any{"resourceUri": mcpURI},
		"openai/widgetAccessible":        true,
		"openai/widgetPrefersBorder":     true,
		"openai/widgetDescription":       description,
		"openai/toolInvocation/invoking": invoking,
		"openai/toolInvocation/invoked":  invoked,
	}
}
