---
layout: lead
index: "01"
kicker: tela · agent-native wiki
title: The wiki your agents <em>reason over</em>.
subtitle: A markdown team wiki with semantic search and a built-in MCP server.
---

---
layout: define
kicker: The idea
term: What is tela?
definition: A <span class="accent2">markdown team wiki</span> your agents search by meaning — then read, write, and cite, right inside Claude or ChatGPT.
points:
  - Real-time multiplayer for the humans
  - Plain markdown you own — hosted or self-hosted
  - SSO, scoped access, and an audit trail
---

---
layout: steps
kicker: How an agent uses it
title: Retrieve, then write back
steps:
  - { title: Search by meaning, desc: semantic_search over heading-aware chunks, icon: "lucide:search" }
  - { title: Read the section, desc: pull the exact chunk that answers, icon: "lucide:book-open" }
  - { title: Write and cite, desc: update the page, cite the source, icon: "lucide:pen-line" }
  - { title: Hand off, desc: the next session picks up there, icon: "lucide:repeat" }
---

---
layout: feature
kicker: What's in the box
title: A real wiki, not an AI gimmick
columns: 2
features:
  - { icon: "lucide:users", title: Live multiplayer, desc: Cursors and edits over Yjs, saved as clean markdown. }
  - { icon: "lucide:search", title: Semantic + full-text, desc: Keyword and vector search, fused and ranked. }
  - { icon: "lucide:plug", title: MCP connector, desc: 24 scoped tools inside Claude and ChatGPT. }
  - { icon: "lucide:file-text", title: Markdown you own, desc: Import a folder, export anytime. Plain files. }
---

---
layout: stats
kicker: By the numbers
title: Built for teams and agents
stats:
  - { value: 24, label: scoped MCP tools, icon: "lucide:wrench", tone: info }
  - { value: 100, unit: "%", label: canonical markdown, icon: "lucide:file-check", tone: good }
  - { value: 0, label: lock-in — export anytime, icon: "lucide:unlock", tone: good }
---

---
layout: code
kicker: Connect it
title: One URL. OAuth or a scoped token.
---

```json
{
  "mcpServers": {
    "tela": {
      "url": "https://tela.cagdas.io/api/mcp",
      "headers": { "Authorization": "Bearer tela_pat_..." }
    }
  }
}
```

---
layout: statement
kicker: Why it matters
title: Your agent stops <em>starting from zero</em>.
---

---
layout: end
title: Try tela
subtitle: Your markdown, hosted or self-hosted.
contact: tela.cagdas.io
---
