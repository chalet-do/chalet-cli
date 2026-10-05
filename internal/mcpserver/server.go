// Package mcpserver is `chalet mcp`: the 37signals gateway (basecamp/mcp)
// serving the tools that the app's catalog describes, each call dispatched
// as one request to the app's JSON API.
package mcpserver

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/basecamp/mcp/gateway"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Config selects what the server serves.
type Config struct {
	// Writes serves the write and destructive tools as well. Without it the
	// server only reads; the token's permission is the hard switch.
	Writes bool
	// Domains narrows to these domains, empty for all. An unknown name stops
	// the server rather than serving less than asked for.
	Domains []string
}

// Server is the MCP server for one account.
type Server struct {
	mcp   *mcp.Server
	Tools []string
}

// New builds the tools from the catalog and hands them to the gateway. A
// server with nothing to serve does not start: an empty tool list reads as
// "Chalet has nothing" to a client, which is never what was meant.
func New(cat *Catalog, api API, confirm Confirmer, cfg Config, version string, logger *slog.Logger) (*Server, error) {
	narrowed, err := cat.Narrow(cfg.Domains)
	if err != nil {
		return nil, err
	}
	domains, err := narrowed.GatewayDomains()
	if err != nil {
		return nil, err
	}

	gw, err := gateway.New(domains, gateway.Config{
		ReadOnly: !cfg.Writes,
		Handler:  handler{api: api, confirm: confirm, logger: logger}.handle,
	})
	if err != nil {
		return nil, err
	}
	if len(gw.Domains()) == 0 {
		return nil, fmt.Errorf("nothing to serve: the chosen domains have no read actions (add --writes to serve their writes)")
	}

	server := gw.BuildMCPServer(&mcp.Implementation{Name: "chalet-cli", Title: "Chalet", Version: version}, logger)
	server.AddReceivingMiddleware(instructions(cat.Rules))

	var tools []string
	for _, d := range gw.Domains() {
		tools = append(tools, d.ToolName())
	}
	return &Server{mcp: server, Tools: tools}, nil
}

// Run serves until the client goes away.
func (s *Server) Run(ctx context.Context, transport mcp.Transport) error {
	session, err := s.mcp.Connect(ctx, transport, nil)
	if err != nil {
		return err
	}
	return session.Wait()
}

// The gateway builds its server without instructions, so the rules are put
// into the handshake on the way out — both the classic initialize and the
// 2026-07-28 discovery.
func instructions(rules string) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			result, err := next(ctx, method, req)
			switch r := result.(type) {
			case *mcp.InitializeResult:
				r.Instructions = rules
			case *mcp.DiscoverResult:
				r.Instructions = rules
			}
			return result, err
		}
	}
}
