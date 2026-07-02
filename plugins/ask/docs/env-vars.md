# Environment Variables

`bee` reads a handful of environment variables. None are required for normal use — they exist for overriding defaults and debugging.

| Variable | Description | Default |
|---|---|---|
| `CB_DB_PATH` | Pin an exact SQLite DB file regardless of how `bee` is launched | `data/cb.db` next to the binary |
| `BEE_DIR` | Override just the root directory used to locate the DB | directory containing the binary |
| `BEE_DEBUG_TRACEBACK` | Set to `1` to enable full stack traces (same as `--debug`) | off |
| `BEE_ASCII` | Set to `1` to force ASCII symbols/borders in the TUI | off (Unicode) |

## `CB_DB_PATH` — share one database

By default each binary location has its own database. To make several copies share one:

```bash
export CB_DB_PATH="$HOME/.bee/cb.db"
bee auth login    # this login is now visible to every bee that reads CB_DB_PATH
```

Useful when you run `bee` both from source (`make dev`) and as a compiled binary and want them to share a login.

## `BEE_DIR` — relocate the data directory

If you want the binary in one place but its `data/` somewhere else:

```bash
export BEE_DIR="$HOME/.bee"
# DB resolves to $HOME/.bee/data/cb.db
```

`CB_DB_PATH` wins over `BEE_DIR` if both are set.

## `BEE_ASCII` — terminals with limited glyphs

If the TUI shows boxes or `?` where icons should be, force ASCII mode:

```bash
BEE_ASCII=1 bee --ui
```

## `BEE_DEBUG_TRACEBACK` — verbose errors

```bash
BEE_DEBUG_TRACEBACK=1 bee job list
# or equivalently
bee --debug job list
```

Prints the full error stack instead of the one-line friendly message — useful when filing a bug or diagnosing a connection problem.
