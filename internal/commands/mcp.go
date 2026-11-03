package commands

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"

	"github.com/chalet-do/chalet-cli/chalet"
	"github.com/chalet-do/chalet-cli/internal/browser"
	"github.com/chalet-do/chalet-cli/internal/config"
	"github.com/chalet-do/chalet-cli/mcpserver"
)

// A seam so tests can drive the server over memory instead of stdin/stdout.
var mcpTransport = func() mcp.Transport { return &mcp.StdioTransport{} }

func mcpCommand() *cobra.Command {
	var cfg mcpserver.Config
	var readOnly bool

	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Serve Chalet to Claude Desktop and Claude Code over stdio",
		Long: "Run an MCP server on stdin/stdout, for Claude Code and Claude Desktop.\n\n" +
			"It reads and writes; --read-only serves the reading tools alone. A trash\n" +
			"or an archive only opens Chalet's confirm page in your browser: nothing\n" +
			"happens until you click there.\n\n" +
			"  claude mcp add --scope user chalet -- chalet mcp",
		Args: cobra.NoArgs,
		Annotations: map[string]string{
			"agent_notes": "Long-running server; stdout speaks the MCP wire protocol. Not for interactive use.",
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg.Writes = !readOnly

			// Logs go to stderr: stdout belongs to the MCP wire.
			logger := slog.New(slog.NewTextHandler(cmd.ErrOrStderr(), nil))

			_, client, err := client()
			if err != nil {
				return err
			}

			catalog, err := loadCatalog(cmd.Context(), client)
			if err != nil {
				return err
			}

			// Without the name the server still serves; the tools answer
			// for themselves if the token no longer works.
			if cfg.Account, err = accountName(cmd.Context(), client); err != nil {
				logger.Warn("could not name the account", "error", err)
			}

			server, err := mcpserver.New(catalog, client, browser.Open, cfg, Version, logger)
			if err != nil {
				return err
			}
			if warning := config.KeychainWarning(); warning != "" {
				logger.Warn(warning)
			}
			logger.Info("serving Chalet over stdio", "url", client.BaseURL, "account", client.Account, "writes", cfg.Writes, "tools", server.Tools)
			return server.Run(cmd.Context(), mcpTransport())
		},
	}
	cmd.Flags().BoolVar(&readOnly, "read-only", false, "serve only the tools that read")
	// The opt-in from when the server only read by default. Accepted and
	// ignored, so a config that still passes it keeps starting.
	cmd.Flags().Bool("writes", true, "")
	_ = cmd.Flags().MarkHidden("writes")
	cmd.Flags().StringSliceVar(&cfg.Domains, "domains", nil, "serve only these domains (comma-separated, e.g. my,todos)")
	return cmd
}

// accountName is the profile's account as "name (url)", as the server names
// it to Claude. Asked without the account in the path, like auth status: the
// identity answers with every account its token reaches.
func accountName(ctx context.Context, client *chalet.Client) (string, error) {
	unscoped := *client
	unscoped.Account = ""
	who, err := whoIs(ctx, &unscoped)
	if err != nil {
		return "", err
	}
	for _, account := range who.Accounts {
		if fmt.Sprint(account.ID) == client.Account {
			return fmt.Sprintf("%s (%s)", account.Name, account.URL), nil
		}
	}
	return "", fmt.Errorf("the token does not reach account %s", client.Account)
}

// loadCatalog reads the app's catalog: every tool chalet serves comes from it.
func loadCatalog(ctx context.Context, client *chalet.Client) (*mcpserver.Catalog, error) {
	resp, err := client.Do(ctx, "GET", "/agent_catalog", nil, nil, nil)
	if err != nil {
		return nil, err
	}
	if !resp.OK() {
		return nil, fmt.Errorf("could not read the catalog: %s", resp.Error())
	}
	return mcpserver.ParseCatalog(resp.Body)
}
