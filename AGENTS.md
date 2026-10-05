# chalet-cli

Guidance for agents working in this repo.

**chalet knows no Chalet feature.** Its tools come from the app's catalog
(`/agent_catalog.json`, built by `Agent::Catalog` in the chalet-app repo) and
nothing else. Never add a command, a tool, an action or a domain by hand, and
never special-case one: when Chalet opens a feature to agents, the app declares
it and this repo does not change. A commit here is for the shape every
operation shares — transport, auth, confirmation, the catalog's format — never
for one operation. That is the proof the design works (api-spec §14.2 in the
app repo), so keep it true.

- The confirmation is the app's: a 428 carries its question and a link to its
  confirm page, where the owner's click does the call. This side only opens
  the link (`internal/browser`) and says so. Never decide here which call is
  dangerous, and never send a 428'd call again.
- Never retry a write. Never follow a redirect. Never print, log or pass the
  token as an argument.
- stdout belongs to the MCP wire in `chalet mcp`; log to stderr.
- Pin `github.com/basecamp/cli` and `github.com/basecamp/mcp` to commits and
  move them deliberately, with the tests as the gate.
