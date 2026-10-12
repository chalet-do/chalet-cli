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
- Files are a shape, like the confirmation: a body field whose items have the
  format `upload` takes local files. `uploadFiles` (`internal/mcpserver`)
  puts each full path up first — the app's direct upload, then the bytes to
  storage without the token (`chalet.Client.Upload`) — and sends the signed
  id in its place. Never a relative path, never a hidden file or one in a
  hidden folder: the path comes from an agent.
- Every error a tool answers is one JSON object, made by `failed`
  (`internal/mcpserver/handler.go`): `ok` (false), `error` (the sentence),
  `code`, `retryable` and `hint`. The codes are stable, because an agent
  branches on them: `bad_request` (this side refused the call: a missing or
  unknown parameter, a file it will not send; the gateway's own refusals get
  this shape in `oneErrorShape`), `unauthorized`, `forbidden`,
  `not_found`, `invalid` (422), `rate_limited` (429), `unavailable` (no
  answer, or a 5xx) and `refused` (any other 4xx). `retryable` is true only
  for no answer, 429, 502, 503 and 504; for a write, the hint adds that chalet
  never sends one twice and the agent should check before it calls again. The
  428 is not an error: it stays the confirmation (`handOver`).
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

`chalet doctor` checks the profile, the token, the catalog, the keychain and
the latest release. Like everything here, it checks shapes, never a feature.

How well a model drives the tools is `TestAgentEval`, with the MCP library's
`eval` package: it makes requests in plain words from the recorded catalog's
tools, asks a model for the call, and grades the answer by rule. Nothing
reaches Chalet. By default the oracle answers, for free, which checks the
harness. To score a model, name it, with an Anthropic API key (it costs a
few cents):

    CHALET_EVAL_MODEL=claude-haiku-4-5 ANTHROPIC_API_KEY=... \
      go test -count=1 -run TestAgentEval -v ./internal/mcpserver

The report is the test's log; a low score fails nothing. With the model set
and no key, the test skips and says so. Refresh the recorded catalog first to
score the tools as the app serves them today.

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

The organization must allow deploy keys (its settings; the API field is
`deploy_keys_enabled_for_repositories`). With them off, GoReleaser publishes
the GitHub release, then fails to push the tap, as v0.1.1 did. Finish such a
release with the next tag: a rerun builds new archives, so the formula's
checksums would not match the files already on the release.

mise needs nothing from this: its `github:` backend installs straight from the
release and picks the archive by its name, so the `chalet_<version>_<os>_<arch>`
names are part of the interface.

Homebrew gets a formula, not the cask GoReleaser now prefers (`goreleaser check`
reports `brews` as deprecated; the release runs regardless). Homebrew
quarantines what a cask downloads, and macOS refuses a quarantined binary that
Apple has not notarized; a formula's download is never quarantined. Notarizing
needs the Apple Developer Program. With it, move to `homebrew_casks` and
`notarize`; the install command then becomes `brew install --cask`.
