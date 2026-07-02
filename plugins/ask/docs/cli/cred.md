# bee cred — Credentials

Manage credentials on the active CloudBees controller. Supports **Username+Password** and **SecretText** types in the system or user store. Requires a controller to be selected first.

---

## Credential stores

| Store | Who can see it | Use for |
|---|---|---|
| `system` (default) | All jobs on this controller | Shared service accounts, deploy keys |
| `user` | Only your own user account | Personal credentials |

Pass `--store user` to any command to target your user store instead.

If you pass `--store user` while not logged in, `bee` warns and falls back to the `system` store.

---

## list

List credentials in the selected store.

```bash
bee cred list [--all] [--store system|user] [-o table|json]
```

| Option | Default | Description |
|---|---|---|
| `--all` | false | Show all credentials (default: Mine list only) |
| `--store` | `system` | Which store to query |
| `-o` / `--output` | `table` | Output format: `table` or `json` |

```bash
bee cred list                        # mine, system store
bee cred list --all                  # everything in system store
bee cred list --all --store user     # everything in your user store
bee cred list --all -o json          # JSON output for scripting
```

---

## get

Show credential metadata. Secrets are never returned by Jenkins — secret fields show `[HIDDEN]`.

```bash
bee cred get <cred_id> [--store system|user]
```

```bash
bee cred get deploy-key
bee cred get my-token --store user
```

---

## create

Create a credential. Two mutually exclusive types:

- **Username+Password** — requires `--username`; `--password` is prompted if omitted.
- **SecretText** — requires `--secret-text`; no `--username`.

```bash
bee cred create \
  [--id <cred_id>] \
  [--description <text>] \
  [--scope GLOBAL|SYSTEM] \
  [--store system|user] \
  --username <username> [--password <password>]   # Username+Password
  # OR
  --secret-text <secret>                          # SecretText
```

| Option | Default | Description |
|---|---|---|
| `--id <id>` | auto (UUID) | Credential ID. Auto-generated if omitted |
| `--username <u>` | — | Username (Username+Password type) |
| `--password <p>` | (prompted) | Password — prompted securely if omitted |
| `--secret-text <s>` | — | Plain secret string (SecretText type) |
| `--description <text>` | `""` | Human-readable description |
| `--scope` | `GLOBAL` | `GLOBAL` (visible to all jobs) or `SYSTEM` (admin only) |
| `--store` | `system` | Target credential store |

```bash
# Username+Password — password prompted
bee cred create --username alice --description "GitHub service account"

# Username+Password with explicit ID and password
bee cred create --id github-alice --username alice --password s3cr3t

# SecretText
bee cred create --id npm-token --secret-text "npm_token_abc123" --description "NPM publish token"

# In user store
bee cred create --username me --store user --id my-token
```

A link to the credential in CloudBees UI is printed on success.

---

## update

Update values on an existing credential. Only the fields you pass change; others are left untouched.

```bash
bee cred update <cred_id> \
  [--username <new_username>] \
  [--password <new_password>] \
  [--secret-text <new_secret>] \
  [--description <new_description>] \
  [--store system|user]
```

`--password` and `--secret-text` are mutually exclusive (a credential is one type or the other).

```bash
bee cred update deploy-key --description "Updated deploy key"
bee cred update npm-token --secret-text "new_token_xyz"
```

---

## delete

Delete one or more credentials. Asks to confirm unless `--yes` is passed.

```bash
bee cred delete <cred_id...> [--yes] [--store system|user]
```

```bash
bee cred delete old-token                      # prompts "Delete credential 'old-token'?"
bee cred delete old-token extra-cred --yes     # skip prompt, delete both
bee cred delete my-token --store user
```

---

## track

Add a credential that already exists on the server to your Mine list.

```bash
bee cred track <cred_id...> [--store system|user]
```

```bash
bee cred track shared-deploy-key
bee cred track key1 key2 key3
```

Verifies the credential exists on the server before tracking. Silently skips ones already tracked.

---

## untrack

Remove one or more credentials from your Mine list. Does **not** delete them from the server.

```bash
bee cred untrack <cred_id...> [--store system|user]
```

```bash
bee cred untrack old-key         # still on server, just no longer "mine"
```

---

## Typical flows

### Create and track a shared deploy key

```bash
bee cred create --id deploy-key --username git --description "Shared deploy key"
# (password prompted)
bee cred list               # confirm it appears in Mine
```

### Rotate a token

```bash
# Update in place — other jobs using the ID keep working
bee cred update npm-token --secret-text "new_npm_token"
```

### Adopt an existing credential someone else created

```bash
bee cred list --all          # find it
bee cred track shared-deploy-key
bee cred list                # now in Mine
```
