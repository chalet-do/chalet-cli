# chalet

Chalet from the command line, and for Claude: `chalet mcp` serves Chalet to
Claude Desktop and Claude Code as a local MCP server, signed in with a personal
access token.

chalet knows no Chalet feature. At start it reads the app's catalog
(`/agent_catalog.json`) and builds its tools from it, so whatever Chalet opens
to agents appears here without a new release of this repo. The tools follow
37signals' gateway convention ([basecamp/mcp](https://github.com/basecamp/mcp)):
one tool per area, called as `{"action": "...", "params": {...}}`, with a
`describe` action for each action's parameters.

## Install

    go install github.com/sschuez/chalet-cli/cmd/chalet@latest

The binary lands in `$(go env GOPATH)/bin/chalet`.

## Sign in

Make a token in Chalet: **Settings › Access tokens › Generate a token**. Start
with *Read*; make a *Read and write* token when you want Claude to change
things. Then:

    chalet auth login --profile dev --url http://localhost:3007

Paste the token when asked. It is read from standard input, never from an
argument, and kept in the macOS keychain. `--account 1` picks the account when
the token reaches more than one. `http://` is accepted for localhost only.

    chalet auth status
    chalet auth logout     # then revoke the token in Chalet's settings too

## Claude Desktop

Add to `~/Library/Application Support/Claude/claude_desktop_config.json`, with
the absolute path (Desktop starts the command without a shell):

```json
{
  "mcpServers": {
    "chalet": {
      "command": "/Users/you/go/bin/chalet",
      "args": ["mcp", "--writes", "--profile", "dev"]
    }
  }
}
```

Quit and reopen Claude Desktop.

## Claude Code

    claude mcp add --scope user chalet -- "$(go env GOPATH)/bin/chalet" mcp --writes --profile dev

Keep `chalet_destructive` off every allow list.

## Safety

- **Read-only unless `--writes`.** The token's own permission is the hard
  switch; this one is the soft one.
- **A token never trashes or archives.** Chalet answers such a call with its
  question and a link to its confirm page; chalet opens the page in your
  browser, and only your click there does it. The link lasts ten minutes and
  works only for you. If no browser opens, the agent hands you the link.
- **A token reaches only what you can reach**, and only the actions Chalet has
  declared for agents. Everything a token changes can be undone.
- **No write is ever retried**, and no redirect is ever followed.

## Development

    go test ./...
    go install ./cmd/chalet

`internal/mcpserver/testdata/catalog.json` is the catalog as the app serves it;
refresh it from a running app when the catalog's shape changes.
