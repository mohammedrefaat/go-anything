// Package mcp implements the Model Context Protocol (MCP) server over stdio,
// exposing agent tools with strict guardrails and input/output sanitization.
package mcp

// New returns a new Server instance.
func New() *Server {
	return NewServer()
}
