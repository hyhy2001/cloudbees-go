# Where data lives — database location

`bee` keeps all local state (login sessions, tracked resources, cache) in a single SQLite file.

## Default location: next to the binary

```
~/.local/bin/bee          ← the binary
~/.local/bin/data/cb.db   ← the database
~/.local/bin/.bee_secret  ← encryption key file (chmod 600)
```

The `data/` directory is created lazily on first run.

## Move the binary — data does NOT follow

If you copy or move `bee` to a new directory without moving `data/`, the new location starts with no login and no tracked resources. Move `data/` alongside the binary to keep your session.

## Two copies = two databases

Running `bee` from source (`make dev`) uses `./data/cb.db` in the project root — a completely separate database from the installed binary. A login under `make dev` is not visible to `dist/bee`.

## Override with environment variables

```bash
# Pin an exact DB file (both copies share the same login)
export CB_DB_PATH="$HOME/.bee/cb.db"

# Override just the root directory
export BEE_DIR="$HOME/.bee"
# DB resolves to $HOME/.bee/data/cb.db
```

`CB_DB_PATH` wins over `BEE_DIR` if both are set. See [Environment Variables](../env-vars.md).

## Security

API tokens are encrypted on disk with **AES-256-GCM**. The key is derived from a random 32-byte secret stored in `.bee_secret` (mode `0600`). A stolen `cb.db` alone cannot recover your token — the attacker also needs `.bee_secret` and must be running as your OS user.
