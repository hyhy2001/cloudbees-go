# Profiles — saved login targets

A **profile** is a saved identity: one CloudBees server URL + username + encrypted API token. Profiles let you stay logged in to multiple servers or accounts at once without re-typing credentials every time.

- The **active profile** is the one all commands use by default.
- The first profile you log in to becomes the default.
- Each profile remembers its own **active controller** separately.

## Switch active profile

```bash
bee auth use <profile>        # switch active profile
bee auth switch <profile>     # alias
```

In the TUI: `Shift+P` opens a profile picker.

## List all profiles

```bash
bee auth profiles
```

The `*` column marks the active profile; the Default column marks the default.

## Create a new profile (log in)

```bash
bee auth login --profile <name>
```

Omit `--profile` to update the `default` profile.

## Delete a profile

```bash
bee auth delete --profile <name>
```

## Log out (keep profile, remove token)

```bash
bee auth logout --profile <name>
```
