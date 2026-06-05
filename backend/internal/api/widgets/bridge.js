// Tela widget host bridge — self-contained, NO external imports.
//
// A previous version imported @modelcontextprotocol/ext-apps from a CDN at
// runtime; Claude's sandboxed iframe blocks that fetch (CSP / sandbox), leaving
// the widget blank. This implements the same two host protocols inline:
//
//   • ChatGPT (Apps SDK): reads window.openai.{toolOutput,theme}.
//   • MCP Apps (Claude / Goose / VS Code): JSON-RPC 2.0 over postMessage, per the
//     SEP-1865 spec — ui/initialize → host result → ui/notifications/initialized,
//     then the host pushes ui/notifications/tool-result with structuredContent.
//
// Exposes window.__telaWidget = { onData(cb), openLink(href) }. onData replays
// the latest payload, so a widget can register its renderer after the bridge has
// already received data (ChatGPT paints synchronously on load).
(function () {
  "use strict";

  var dataCb = null;
  var lastData = null;

  function applyTheme(t) {
    if (t === "dark" || t === "light") document.documentElement.dataset.theme = t;
  }
  function emit(data) {
    lastData = data || {};
    if (dataCb) dataCb(lastData);
  }

  var api = {
    onData: function (cb) { dataCb = cb; if (lastData) cb(lastData); },
    openLink: function () {}, // set by the active host branch below
  };
  window.__telaWidget = api;

  // ── ChatGPT (Apps SDK) ──────────────────────────────────────────────────
  if (window.openai) {
    api.openLink = function (href) {
      if (window.openai.openExternal) window.openai.openExternal({ href: href });
    };
    var paint = function () { applyTheme(window.openai.theme); emit(window.openai.toolOutput || {}); };
    paint(); // globals may already be present
    window.addEventListener("openai:set_globals", function (e) {
      var g = e.detail && e.detail.globals;
      if (g && ("toolOutput" in g || "theme" in g)) paint();
    });
    // …or arrive shortly after; poll briefly, then give up.
    var tries = 0;
    var poll = setInterval(function () {
      if (window.openai.toolOutput || ++tries > 40) { clearInterval(poll); paint(); }
    }, 250);
    return;
  }

  // ── MCP Apps standard (Claude / Goose / VS Code) ────────────────────────
  // The guest UI is an MCP client over a postMessage transport. parent is the
  // sandbox proxy that relays to the host; targetOrigin "*" is the spec example.
  var nextId = 1;
  function send(msg) { window.parent.postMessage(msg, "*"); }
  function request(method, params) {
    var id = nextId++;
    send({ jsonrpc: "2.0", id: id, method: method, params: params || {} });
    return id;
  }
  function notify(method, params) {
    send({ jsonrpc: "2.0", method: method, params: params || {} });
  }

  api.openLink = function (href) { request("ui/open-link", { url: href }); };

  var initId = request("ui/initialize", {
    appInfo: { name: "tela-widget", version: "1.0.0" },
    appCapabilities: {},
  });

  window.addEventListener("message", function (ev) {
    var m = ev.data;
    if (!m || m.jsonrpc !== "2.0") return;

    // Response to our ui/initialize: ack with initialized, then the host is
    // allowed to push the tool result.
    if (m.id === initId && m.result) {
      applyTheme((m.result.hostContext || {}).theme);
      notify("ui/notifications/initialized", {});
      return;
    }
    if (m.method === "ui/notifications/tool-result") {
      emit((m.params || {}).structuredContent || {});
      return;
    }
    if (m.method === "ui/notifications/host-context-changed") {
      applyTheme(((m.params || {}).hostContext || {}).theme);
      if (lastData) emit(lastData); // re-render under the new theme
    }
  });
})();
