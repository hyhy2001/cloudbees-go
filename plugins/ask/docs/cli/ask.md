# bee ask — Binary Help Search

Need exact `bee` command but do not remember syntax? `bee ask` searches help built into binary and returns short, actionable answer. It does not require source tree, external docs, or network access.

```bash
bee ask <query...> [--limit 5]
```

| Argument / Option | Default | Description |
|---|---|---|
| `<query...>` | — | What you want to do, in plain words |
| `--limit <n>` | `5` | Max matches to retrieve before formatting answer |
| `--json` | `false` | Output machine-readable JSON with answer and matched items |

`bee ask` uses local search over:
- **Live command tree** — exact commands compiled into current binary
- **Curated help facts** — built-in concepts and troubleshooting answers

For action queries, it prefers exact commands. For concept or error queries, it prefers short built-in explanations plus related commands.

## Examples

```bash
bee ask "stop a build"
```

```text
Best match: bee job stop <name>
Stop a running build

Usage:
  bee job stop <name>
```

```bash
bee ask "what is a profile"
```

```text
A profile is a saved login target for one CloudBees server, including server URL, username, and encrypted token.

Related commands:
  bee auth profiles
  bee auth use <profile>
  bee auth login --profile <profile>
```

More queries:

```bash
bee ask "switch profile"
bee ask "403 error"
bee ask "ssh node"
bee ask "delete credential"
bee ask "switch profile" --json
```

## Matching behavior

Search uses local SQLite FTS5 with:
1. command-title weighting
2. stopword filtering
3. synonym expansion
4. relevance gate for weak matches

Examples of synonym handling:
- `kill build` → `stop job`
- `agent` → `node`
- `remove credential` → `delete credential`

If nothing matches, `bee ask` suggests shorter keywords or `bee --help`.

## JSON output

Use `--json` for scripts, wrappers, or agent tooling:

```bash
bee ask "switch profile" --json
```

Response includes query, answer text, source mode, and matched hits.
