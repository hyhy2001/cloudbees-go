# bee controller — Controller Selection

A CloudBees CI server hosts one or more **controllers** (managed Jenkins masters). Most operations — jobs, nodes, credentials — run against one specific controller, so you pick an **active controller** first. The selection is remembered per profile.

See [Controllers concept](../concepts/controllers.md) for the profile ↔ controller relationship.

---

## list

List all controllers on the CloudBees server. The active one is marked with `*`.

```bash
bee controller list
```

```
┌────────┬──────────┬──────────────────────┬─────────┐
│ Active │ Name     │ Description          │ Status  │
├────────┼──────────┼──────────────────────┼─────────┤
│ *      │ prod     │ Production builds    │ ONLINE  │
│        │ staging  │ Staging              │ ONLINE  │
└────────┴──────────┴──────────────────────┴─────────┘
```

---

## info

Show one controller's details, including which resource types you have permission to create on it.

```bash
bee controller info <name>
```

| Argument | Description |
|---|---|
| `<name>` | Controller name |

Reported fields include `url`, `typeLabel`, `online`, and the creation permissions `canCreateJob` / `canCreateNode` / `canCreateCred`.

```bash
bee controller info prod
```

---

## select

Set the active controller. Every subsequent job/node/cred command targets it until you select another (or switch profiles).

```bash
bee controller select <name>
```

| Argument | Description |
|---|---|
| `<name>` | Controller name (must exist in `controller list`) |

```bash
bee controller select prod
```

The controller's real URL is resolved and stored, so later commands hit it directly.

---

## current

Show the currently active controller and its resolved URL.

```bash
bee controller current
```

```
Active controller: prod
URL              : https://ci.acme.com/prod/
```

If nothing is selected yet, it tells you to run `bee controller select <name>`.

---

## Typical flow

```bash
bee controller list             # see what's available
bee controller select prod      # pick one
bee controller current          # confirm
bee job list                    # now scoped to prod
```
