# Getting Started

This guide takes you from a fresh binary to your first job in a few minutes. It assumes nothing — if you've never touched `bee`, start here.

## 1. Install

You have the pre-built binary `bee` (or `dist/bee` from a build). No runtime, no dependencies needed.

```bash
# Copy it somewhere on your PATH
cp dist/bee ~/.local/bin/bee

# Or let bee install itself (creates a wrapper + symlink in ~/.local/bin)
./dist/bee --install
```

Confirm it runs:

```bash
bee --version
```

> **Important — where your data lives:** `bee` stores its login + local state in a SQLite database that sits **next to the binary**, at `<bin dir>/data/cb.db`. The directory you put `bee` in must be **writable**. Move the binary and the data does not follow unless you move `data/` too. See [Where data lives](concepts/data-location.md).

## 2. First login to CloudBees server

You need three things from your CloudBees server:

- **Server URL** — e.g. `https://cloudbees.example.com`
- **Username** — your CloudBees/Jenkins username
- **API Token** — generate one in CloudBees under *Your Profile → Security → API Token* (not your password)

```bash
bee auth login
```

It prompts for each value (the token input is hidden). Or pass them as flags:

```bash
bee auth login --url https://cloudbees.example.com --username alice --token <api_token>
```

On success you'll see `OK Logged in as 'alice'`. Your token is encrypted before it touches disk (see [Where data lives → Security](concepts/data-location.md#security)).

## 3. Select a controller

A CloudBees server hosts one or more **controllers** (the actual Jenkins masters that run jobs). Pick which one your commands target:

```bash
bee controller list             # see what's available
bee controller select <name>    # set the active one
bee controller current          # confirm
```

This choice is remembered per profile — you only do it once per controller.

## 4. Your first workflow

```bash
# See your jobs (empty at first — "Mine" list, see Concepts)
bee job list

# List everything on the controller instead
bee job list --all

# Create a simple Freestyle job
bee job create freestyle hello --shell "echo hello world"

# Run it and wait for the result
bee job run hello --wait

# Read the console log
bee job log hello
```

## 5. Try the TUI

Everything above is also available in an interactive UI:

```bash
bee --ui
```

Use `Tab` / number keys to switch tabs, `↑`/`↓` to move, `Enter` to act, `?` for help, `Ctrl+q` to quit. Full guide: [TUI](tui.md).

## Lost? Search for a command

```bash
bee ask "run a build and wait"
bee ask "delete credential"
bee ask "switch profile" --json
```

`bee ask` searches help built into binary and returns exact commands or short built-in explanations. See [ask](cli/ask.md).

## Next steps

- [Profiles](concepts/profiles.md) — managing multiple logins
- [Mine vs All](concepts/mine-vs-all.md) — tracked resources
- [CLI Reference](index.md) — every command group in detail
- [Troubleshooting](troubleshooting/auth.md) — when something doesn't work
