# chalet-cli

Guidance for agents working in this repo. The README is for the people who
install chalet: installing, signing in, connecting Claude. How chalet works and
how to change it belongs here.

**chalet knows no Chalet feature.** Its tools come from the app's catalog
(`/agent_catalog.json`, built by `Agent::Catalog` in the chalet-app repo) and
nothing else. Never add a command, a tool, an action or a domain by hand, and
never special-case one: when Chalet opens a feature to agents, the app declares
it and this repo does not change. A commit here is for the shape every
operation shares — transport, auth, confirmation, the catalog's format — never
for one operation. That is the proof the design works (api-spec §14.2 in the
app repo), so keep it true.

The tools follow the gateway convention of the MCP library in `go.mod`: one
tool per area, called as `{"action": "...", "params": {...}}`, with a
`describe` action for each action's parameters.

- The confirmation is the app's: a 428 carries its question and a link to its
  confirm page, where the owner's click does the call. This side only opens
  the link (`internal/browser`) and says so. Never decide here which call is
  dangerous, and never send a 428'd call again.
- Never retry a write. Never follow a redirect. Never print, log or pass the
  token as an argument.
- stdout belongs to the MCP wire in `chalet mcp`; log to stderr.
- The CLI and MCP libraries in `go.mod` are pinned to commits; move them
  deliberately, with the tests as the gate.

## Development

    go test ./...
    go install ./cmd/chalet

Against the app running locally (`http://` is accepted for localhost only):

    chalet auth login --profile dev --url http://localhost:3007 --account 1
    claude mcp add --scope user chalet-dev -- "$(go env GOPATH)/bin/chalet" mcp --writes --profile dev

`internal/mcpserver/testdata/catalog.json` is the catalog as the app serves it;
refresh it from a running app when the catalog's shape changes.

## Releasing

Push a version tag. `.github/workflows/release.yml` runs the tests on Linux,
macOS and Windows (`test.yml`, which also runs on every pull request), then
GoReleaser (`.goreleaser.yaml`): the archives and the `.deb` and `.rpm`
packages go on the GitHub release, and the Homebrew formula and the Scoop
manifest into `chalet-do/homebrew-tap`. It writes there with a deploy key: the
secret `TAP_DEPLOY_KEY` here, whose public half is the tap's one deploy key.

    git tag v0.1.0 && git push origin v0.1.0

Try it without publishing anything:

    TAP_DEPLOY_KEY=unused goreleaser release --snapshot --clean

Homebrew gets a formula, not the cask GoReleaser now prefers (`goreleaser check`
reports `brews` as deprecated; the release runs regardless). Homebrew
quarantines what a cask downloads, and macOS refuses a quarantined binary that
Apple has not notarized; a formula's download is never quarantined. Notarizing
needs the Apple Developer Program. With it, move to `homebrew_casks` and
`notarize`; the install command then becomes `brew install --cask`.
