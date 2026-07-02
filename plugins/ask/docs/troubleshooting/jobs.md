# Jobs and builds — tracked jobs disappeared, deleted on server, build log slow

## My tracked jobs disappeared — DELETED_ON_SERVER

Tracking is per (resource type, profile, controller). If you switched profiles or controllers, your Mine list for the previous combination is still there — switch back:

```bash
bee auth use <profile>
bee controller select <controller>
bee job list                   # Mine list for this profile+controller
```

If you moved the binary without moving `data/`, the database didn't follow. See [Where data lives](../concepts/data-location.md).

## `[DELETED_ON_SERVER]` entries in job list

A job was deleted from the server but still appears in your Mine list. Clean it up:

```bash
bee job untrack <name>
```

## Build log `--follow` is very slow

Progressive log streaming polls every 3 seconds by design to avoid hammering the server. The TUI log viewer (`Enter → View Log`) uses the same interval. This is expected behaviour.

## `bee ask` returns no results

The query has no close match in the live command tree. Try broader terms:

```bash
bee ask job           # instead of "create freestyle job with params"
bee ask credential    # instead of "add secret"
bee --help            # raw commander help tree
```
