-- Per-user "last authenticated MCP request" stamp. Written by mcpVerifier on
-- both the PAT and OAuth paths, so it's the one signal that detects an MCP
-- connection regardless of credential (OAuth/cowork leaves no api_keys trace).
-- NULL = never connected an MCP client.
ALTER TABLE users ADD COLUMN mcp_last_seen_at TEXT;
