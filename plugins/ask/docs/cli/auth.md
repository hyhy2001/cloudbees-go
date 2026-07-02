# bee auth — Authentication & Profiles

Manage logins. A **profile** is a named bundle of (controller URL + username + encrypted API token). You can be logged in to several controllers at once via different profiles; the **active profile** is the one every other command uses.

See [Profiles concept](../concepts/profiles.md) for how profiles and controllers relate.

---

## login

Log in and save a profile's session. Also makes that profile active.

```bash
bee auth login \
  --url <cloudbees_url> \
  --username <username> \
  --token <api_token> \
  [--profile default]
```

| Option | Default | Description |
|---|---|---|
| `--url <url>` | (prompted) | CloudBees server URL |
| `--username <name>` | (prompted) | Your username |
| `--token <token>` | (prompted, hidden) | Your API token |
| `--profile <name>` | `default` | Profile name to save under |

Any of `--url`, `--username`, `--token` you omit are prompted for interactively. The token prompt hides input.

**Where to get an API token:** in CloudBees/Jenkins, your user menu → Configure → API Token → Add new token.

```bash
# Interactive — prompts for everything
bee auth login

# A second profile for a different server
bee auth login --url https://ci.example.com --username alice --profile staging
```

The first profile you create becomes the default. Re-logging into an existing profile keeps its default status.

---

## use (alias: switch)

Switch which profile is active.

```bash
bee auth use <profile>
bee auth switch <profile>      # same thing
```

Switching also restores that profile's remembered active controller.

```bash
bee auth use staging
```

Fails if the named profile has no saved session — log in first.

---

## profiles

List all saved profiles. The active one is marked with `*` in the Active column; the default one is marked in the Default column.

```bash
bee auth profiles
```

```
┌────────┬─────────┬──────────────────────┬──────────┬─────────┐
│ Active │ Profile │ Server               │ Username │ Default │
├────────┼─────────┼──────────────────────┼──────────┼─────────┤
│ *      │ default │ https://ci.acme.com  │ alice    │ *       │
│        │ staging │ https://ci.stg.com   │ alice    │         │
└────────┴─────────┴──────────────────────┴──────────┴─────────┘
```

---

## logout

Remove the stored token for a profile (the profile itself is kept).

```bash
bee auth logout [--profile <name>]
```

| Option | Default | Description |
|---|---|---|
| `--profile <name>` | active profile | Which profile to log out |

```bash
bee auth logout                  # log out of the active profile
bee auth logout --profile staging
```

---

## delete

Delete a saved profile entirely (clears its session first, then removes it).

```bash
bee auth delete --profile <name>
```

| Option | Required | Description |
|---|---|---|
| `--profile <name>` | yes | Profile name to delete |

```bash
bee auth delete --profile staging
```

---

## Typical flow

```bash
bee auth login --url https://ci.acme.com --username alice    # token prompted
bee auth profiles                                            # confirm it saved
bee controller list                                          # now use it
```
