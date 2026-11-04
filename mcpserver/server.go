// Package mcpserver is the tool layer of `chalet mcp` and of
// chalet-mcp-server: the toolkit's gateway (basecamp/mcp) serving the tools
// that the app's catalog describes, each call dispatched as one request to
// the app's JSON API — and chalet_picture beside them, which hands over a
// picture that an answer links to. The transport is the caller's: stdio here,
// HTTP in chalet-mcp-server.
package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/basecamp/mcp/gateway"
	"github.com/basecamp/mcp/result"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chalet-do/chalet-cli/chalet"
)

// The most a text result may weigh, in bytes as it goes on the wire: Claude
// takes about 150,000 characters of one, and a character is at least a byte.
const resultLimit = 140_000

// Config selects what the server serves.
type Config struct {
	// Writes serves the write and destructive tools as well: `chalet mcp`
	// sets it unless --read-only, a server for others when the grant may
	// write. The token's permission is the hard switch.
	Writes bool
	// Domains narrows to these domains, empty for all. An unknown name stops
	// the server rather than serving less than asked for.
	Domains []string
	// Account is the account this server reaches. Its name and URL go after
	// the rules' first paragraph, so that Claude can tell this server from
	// one for another account (a profile each). None leaves them out.
	Account chalet.Account
	// SignIn is the hint of an unauthorized answer: how the person signs in
	// again. Empty says to make a new token and run `chalet auth login`.
	SignIn string
}

// Server is the MCP server for one account. MCP is what a transport serves:
// stdio in Run, or any other the caller connects it to.
type Server struct {
	MCP   *mcp.Server
	Tools []string
}

// New builds the tools from the catalog and hands them to the gateway. A
// server with nothing to serve does not start: an empty tool list reads as
// "Chalet has nothing" to a client, which is never what was meant.
func New(cat *Catalog, api API, open Opener, cfg Config, version string, logger *slog.Logger) (*Server, error) {
	narrowed, err := cat.Narrow(cfg.Domains)
	if err != nil {
		return nil, err
	}
	// A copy: Narrow hands back the catalog itself when it keeps everything.
	// The account goes after the rules' first paragraph, not in front: some
	// clients read only the first 512 characters, and that paragraph must be
	// whole in them, however long the account's name.
	if cfg.Account.Name != "" {
		named := *narrowed
		opening, rest, _ := strings.Cut(named.Rules, "\n\n")
		account := "This server reaches one Chalet account: " + cfg.Account.Name + " (" + cfg.Account.URL + "). If the owner connected other Chalet accounts as other servers, use the one the request is about, and ask when it is unclear."
		named.Rules = strings.TrimSpace(opening + "\n\n" + account + "\n\n" + rest)
		narrowed = &named
	}
	domains, err := narrowed.GatewayDomains()
	if err != nil {
		return nil, err
	}

	h := handler{api: api, open: open, logger: logger, signIn: cfg.SignIn}
	gw, err := gateway.New(domains, gateway.Config{
		ReadOnly: !cfg.Writes,
		Handler:  h.handle,
	})
	if err != nil {
		return nil, err
	}
	if len(gw.Domains()) == 0 {
		return nil, fmt.Errorf("nothing to serve: the chosen domains have no read actions (drop --read-only to serve their writes)")
	}

	server := gw.BuildMCPServer(&mcp.Implementation{Name: "chalet-cli", Title: "Chalet", Version: version}, logger)
	server.AddReceivingMiddleware(instructions(narrowed.Rules), oneErrorShape, capped)

	// Whatever the domains: a picture can sit in the words of any of them.
	addPictureTool(server, h)

	var tools []string
	for _, d := range gw.Domains() {
		tools = append(tools, d.ToolName())
	}
	return &Server{MCP: server, Tools: append(tools, pictureTool)}, nil
}

// Run serves until the client goes away.
func (s *Server) Run(ctx context.Context, transport mcp.Transport) error {
	session, err := s.MCP.Connect(ctx, transport, nil)
	if err != nil {
		return err
	}
	return session.Wait()
}

// The gateway refuses some calls itself, in plain words — an unknown action,
// a write when serving read-only — and those get the shape of every other
// error (failed). A call of an unknown tool comes back as a nil result.
func oneErrorShape(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		result, err := next(ctx, method, req)
		if r, ok := result.(*mcp.CallToolResult); ok && r != nil && r.IsError && len(r.Content) == 1 {
			if text, ok := r.Content[0].(*mcp.TextContent); ok && !json.Valid([]byte(text.Text)) {
				return failed(nil, errors.New(text.Text), false), err
			}
		}
		return result, err
	}
}

// capped holds a text result under resultLimit, cut where the cut can be
// explained (the toolkit's result.Cap). Not a picture: the cap would withhold
// it whole, and it has a bound of its own (pictureLimit).
func capped(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		res, err := next(ctx, method, req)
		if r, ok := res.(*mcp.CallToolResult); ok && r != nil && !(len(r.Content) == 1 && isImage(r.Content[0])) {
			return result.Cap(r, resultLimit), err
		}
		return res, err
	}
}

func isImage(c mcp.Content) bool {
	_, ok := c.(*mcp.ImageContent)
	return ok
}

// The gateway builds its server without instructions, so the rules are put
// into the handshake on the way out — both the classic initialize and the
// 2026-07-28 discovery. A refused handshake, such as a second initialize,
// comes back as a nil result.
func instructions(rules string) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			result, err := next(ctx, method, req)
			switch r := result.(type) {
			case *mcp.InitializeResult:
				if r != nil {
					r.Instructions = rules
				}
			case *mcp.DiscoverResult:
				if r != nil {
					r.Instructions = rules
				}
			}
			return result, err
		}
	}
}
