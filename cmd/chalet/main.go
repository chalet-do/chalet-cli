// chalet is Chalet's command line: sign in with a personal access token, and
// serve Chalet to Claude Desktop and Claude Code over MCP.
package main

import (
	"os"

	"github.com/chalet-do/chalet-cli/internal/commands"
)

func main() {
	os.Exit(commands.Execute())
}
