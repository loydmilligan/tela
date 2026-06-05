package api

// apiErr is the transport-agnostic error returned by the extracted handler
// "core" functions (the xCore funcs the REST routes and the MCP tools both
// call). The HTTP layer maps it via writeError; the MCP layer maps it to a
// CallToolResult error envelope. Code is the load-bearing machine-readable
// field — agents key behavior on it, so it must match the REST `code` strings.
type apiErr struct {
	Status  int
	Code    string
	Message string
}

func (e *apiErr) Error() string { return e.Message }
