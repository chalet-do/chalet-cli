# chalet

Connect Claude to your [Chalet](https://chalet.do). Claude can then look things
up in Chalet for you and, if you allow it, change them.

## Install

**macOS and Linux**, with [Homebrew](https://brew.sh):

    brew install chalet-do/tap/chalet

**Windows**, with [Scoop](https://scoop.sh):

    scoop bucket add chalet-do https://github.com/chalet-do/homebrew-tap
    scoop install chalet

**Debian, Ubuntu, Fedora or RHEL**: download the `.deb` or `.rpm` for your
system from the [latest release](https://github.com/chalet-do/chalet-cli/releases/latest),
and install it:

    sudo apt install ./chalet_*.deb     # Debian, Ubuntu
    sudo dnf install ./chalet_*.rpm     # Fedora, RHEL

**Without a package manager** (Linux, Windows): download the archive for your
system from the same page, unpack it, and move `chalet` (`chalet.exe` on
Windows) to a folder on your `PATH`.

Check that it works:

    chalet --version

## Sign in

1. In Chalet, open **Settings › Access tokens** and generate a token. A *Read*
   token lets Claude look; a *Read and write* token lets it change things too.
2. Sign in, and paste the token when asked:

       chalet auth login

The token is kept in your system's keychain. If it reaches more than one
account, add `--account` with the account's number, as in `/1/` in your Chalet
links.

    chalet auth status    # who you are signed in as
    chalet auth logout    # forget the token, then revoke it in Chalet too

## Connect Claude Code

    claude mcp add --scope user chalet -- chalet mcp --writes

Leave out `--writes` to let Claude only read.

## Connect Claude Desktop

In Claude Desktop, open **Settings › Developer › Edit Config**, and add chalet
to `claude_desktop_config.json` with its full path:

```json
{
  "mcpServers": {
    "chalet": {
      "command": "/opt/homebrew/bin/chalet",
      "args": ["mcp", "--writes"]
    }
  }
}
```

`which chalet` prints the path on macOS, and `where.exe chalet` on Windows
(write each `\` in it as `\\`). Then quit and reopen Claude Desktop.

## What Claude can do

- Claude reaches only what you can reach in Chalet.
- Without `--writes`, Claude only reads. A *Read* token never writes, whatever
  the flag says.
- Claude never trashes or archives anything by itself. It opens Chalet's
  confirm page in your browser, and nothing happens until you click there. The
  link works for ten minutes, and only for you.

## Update and remove

    brew upgrade chalet         # Scoop: scoop update chalet
    brew uninstall chalet       # Scoop: scoop uninstall chalet

When you remove chalet, revoke its token in Chalet's settings as well.
