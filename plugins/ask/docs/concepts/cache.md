# Cache — response caching and TTLs

`bee` caches server responses in the local SQLite DB so repeated calls and TUI tab-switches don't refetch everything from scratch.

## Cache TTLs by resource

| Resource | Cache duration |
|---|---|
| Job list | 15 seconds |
| Job details | 20 seconds |
| Controller list / details | 60 seconds |
| Controller capabilities | 5 minutes |
| Nodes / credentials | 30 seconds |

## Writes invalidate cache immediately

Create, update, and delete operations call `invalidatePrefix(...)` — you always see the current state after a write. The stale list entry is dropped instantly.

## Force a fresh fetch in TUI

Press `r` on any tab to refetch now, ignoring the cached value.

## Clear entire cache

On the TUI Info tab, press `Ctrl+x` to wipe the entire cache. All next requests go to the server.

## Where the cache lives

The cache is a table in the local SQLite DB at `<bin dir>/data/cb.db`. Deleting the DB clears the cache (and your login/tracked resources).
