package mcpserver

import (
	"context"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	serverName            = "engram"
	serverVersion         = "v0.1.0"
	offlineDegradedReason = "no embedding endpoint configured (offline mode)"
)

// NewServer builds the MCP server and registers the MVP memory tools.
func NewServer(registry *Registry) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: serverName, Version: serverVersion}, nil)
	adapter := &toolAdapter{registry: registry}
	registerTools(server, adapter)
	return server
}

// Run serves one MCP stdio session until the client disconnects or ctx ends.
func Run(ctx context.Context, registry *Registry) error {
	if registry == nil {
		return errors.New("nil registry")
	}
	return NewServer(registry).Run(ctx, &mcp.StdioTransport{})
}

func registerTools(server *mcp.Server, adapter *toolAdapter) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "memory_write",
		Description: "Write or update one memory entry in a namespace.",
	}, adapter.memoryWrite)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "memory_search",
		Description: "Search memories by relevance in a namespace.",
	}, adapter.memorySearch)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "memory_list",
		Description: "List all memory entries in a namespace.",
	}, adapter.memoryList)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "memory_get",
		Description: "Get one memory entry by name from a namespace.",
	}, adapter.memoryGet)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "memory_delete",
		Description: "Delete one memory entry by name from a namespace.",
	}, adapter.memoryDelete)
}
