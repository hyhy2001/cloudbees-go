# Mine vs All — tracked resources

By default, `job list`, `node list`, and `cred list` show only **your resources** — things you created through `bee` or explicitly tracked. This is the **Mine list**.

## Show all resources on the server

```bash
bee job list --all
bee node list --all
bee cred list --all
```

## Add an existing server resource to Mine

```bash
bee job track <name>
bee node track <name>
bee cred track <cred_id>
```

## Remove from Mine (does NOT delete from server)

```bash
bee job untrack <name>
bee node untrack <name>
bee cred untrack <cred_id>
```

## Toggle Mine / All in TUI

Press `Ctrl+a` on any tab to switch between Mine and All views. The setting is remembered per tab.

## `[DELETED_ON_SERVER]` entries

A resource that was deleted from the server still appears in your Mine list as `[DELETED_ON_SERVER]`. Clean it up with `untrack`.

## Tracking scope

Tracking is per (resource type, profile, controller). The same job name tracked under two different controllers are independent entries. Switching profiles shows a different Mine list.
