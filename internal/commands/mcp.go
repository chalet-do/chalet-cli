package commands

import (
	"fmt"
	"log/slog"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"

	"github.com/sschuez/chalet-cli/internal/config"
	"github.com/sschuez/chalet-cli/internal/dialog"
	"github.com/sschuez/chalet-cli/internal/mcpserver"
)

// A seam so tests can drive the server over memory instead of stdin/stdout.
var mcpTransport = func() mcp.Transport { return &mcp.StdioTransport{} }

func mcpCommand() *cobra.Command {
	var cfg mcpserver.Config

	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Serve Chalet to Claude Desktop and Claude Code over stdio",
		Long: "Run an MCP server on stdin/stdout. Its tools come from the app's catalog\n" +
			"(/agent_catalog.json), so whatever Chalet opens to agents appears here.\n\n" +
			"Read-only unless --writes. Trash, archive and writes that clients will see\n" +
			"first ask you in a dialog on this Mac.\n\n" +
			"  claude mcp add --scope user chalet -- \"$(command -v chalet)\" mcp --writes --profile dev",
		Args: cobra.NoArgs,
		Annotations: map[string]string{
			"agent_notes": "Long-running server; stdout speaks the MCP wire protocol. Not for interactive use.",
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Logs go to stderr: stdout belongs to the MCP wire.
			logger := slog.New(slog.NewTextHandler(cmd.ErrOrStderr(), nil))

			_, client, err := client()
			if err != nil {
				return err
			}

			resp, err := client.Do(cmd.Context(), "GET", "/agent_catalog", nil, nil, nil)
			if err != nil {
				return err
			}
			if !resp.OK() {
				return fmt.Errorf("could not read the catalog: %s", resp.Error())
			}
			catalog, err := mcpserver.ParseCatalog(resp.Body)
			if err != nil {
				return err
			}

			server, err := mcpserver.New(catalog, client, dialog.Confirm, cfg, Version, logger)
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
	cmd.Flags().BoolVar(&cfg.Writes, "writes", false, "serve the tools that change things as well")
	cmd.Flags().StringSliceVar(&cfg.Domains, "domains", nil, "serve only these domains (comma-separated, e.g. my,todos)")
	return cmd
}
