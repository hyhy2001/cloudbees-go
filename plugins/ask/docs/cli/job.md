# bee job — Jobs

Manage jobs and folders on the active CloudBees controller. Covers the full lifecycle: list, inspect, create, update, run, monitor, and organize. Requires a controller to be selected first (`bee controller select <name>`).

Job types `bee` knows about:

- **FS** — Freestyle project (a shell-command job; `bee` can create/update these)
- **FD** — Folder (a container for other jobs)
- **PL** — Pipeline (listed and run, but created via the CloudBees UI)

---

## list

```bash
bee job list [--all] [--recursive]
```

| Option | Default | Description |
|---|---|---|
| `--all` | false | Show all jobs on the controller (default: your Mine list) |
| `--recursive` | false | Descend into folders and list jobs at every level |

```bash
bee job list                  # mine, top level
bee job list --all            # everything the controller exposes
bee job list --all --recursive   # everything, folders expanded
```

Jobs you track but that were deleted on the server show as `[DELETED_ON_SERVER]`.

---

## get

Show job details plus a config summary (schedule, email settings, parameters).

```bash
bee job get <name>
```

```bash
bee job get build-api
bee job get my-folder/build-api      # job inside a folder
```

---

## create freestyle

Create a Freestyle (shell-command) job.

```bash
bee job create freestyle <name> \
  [--description <text>] \
  [--shell <command>] \
  [--chdir <directory>] \
  [--node <label_or_node>] \
  [--folder <parent_folder>] \
  [--schedule "<cron>"] \
  [--param-def NAME=default ...] \
  [--email "a@x.com,b@y.com"] \
  [--email-cond failed|success|always|custom] \
  [--email-keyword <kw> ...] \
  [--email-regex "<regex>"]
```

| Option | Default | Description |
|---|---|---|
| `--description <text>` | `""` | Job description |
| `--shell <cmd>` | `echo hello` | Shell command to run |
| `--chdir <dir>` | — | Working directory for the script |
| `--node <node>` | (any) | Restrict to a node or label |
| `--folder <path>` | (root) | Parent folder, e.g. `team/backend` |
| `--schedule "<cron>"` | — | Cron schedule, e.g. `"H 8 * * *"` |
| `--param-def NAME=default` | — | Add a String build parameter (repeatable) |
| `--email <emails>` | — | Comma-separated notification recipients |
| `--email-cond <cond>` | `failed` | When to email: `failed`, `success`, `always`, `custom` |
| `--email-keyword <kw>` | — | Email only if log contains keyword (repeatable) |
| `--email-regex <regex>` | — | Email only if log matches regex |

```bash
# Minimal
bee job create freestyle hello --shell "echo hi"

# On a specific node, daily at 8am
bee job create freestyle nightly \
  --shell "make build" --node linux --schedule "H 8 * * *"

# With build parameters
bee job create freestyle deploy \
  --shell 'deploy.sh $ENV' \
  --param-def ENV=staging --param-def VERSION=latest

# Inside a folder, with failure email
bee job create freestyle api-build \
  --folder team/backend \
  --shell "make api" \
  --email "team@corp.com" --email-cond failed
```

If you don't pass `--node`, the job runs on any available agent (a warning is printed).

### String parameters

`--param-def NAME=default` is repeatable — each adds one String parameter. Run with `bee job run <name> -p NAME=value`.

### Email anti-spam filter

The email options build a content filter so you only get mail that matters:

- `--email-keyword` is repeatable; matches **any** keyword in the build log.
- `--email-regex` matches the log with a case-insensitive regex.
- With both, the content condition is `keyword OR regex`.
- Mail is sent only when **both** the trigger (`--email-cond`) fired **and** the content filter matched.
- A filter with no `--email` recipient fails fast.

---

## create folder

```bash
bee job create folder <name> [--description <text>] [--folder <parent>]
```

```bash
bee job create folder team
bee job create folder backend --folder team       # creates team/backend
```

---

## update freestyle

Partial update — only the flags you pass change. Pass an empty string to clear a field (e.g. `--schedule ""`).

```bash
bee job update freestyle <name> \
  [--description <text>] \
  [--shell <command>] \
  [--chdir <directory>] \
  [--node <label_or_node>] \
  [--schedule "<cron>|''"] \
  [--param-def NAME=default ...] [--clear-params] \
  [--email "...|''"] \
  [--email-cond failed|success|always|custom] \
  [--email-keyword <kw> ...] [--clear-email-keywords] \
  [--email-regex "<regex>"] [--clear-email-regex]
```

| Option | Effect |
|---|---|
| `--email-cond <cond>` | Change the email trigger: `failed`, `success`, `always`, or `custom` |
| `--clear-params` | Remove all String parameters |
| `--clear-email-keywords` | Remove all email keyword filters |
| `--clear-email-regex` | Remove the email regex filter |

```bash
bee job update freestyle nightly --schedule "H 2 * * *"   # change schedule
bee job update freestyle nightly --schedule ""            # remove schedule
bee job update freestyle deploy --clear-params            # drop all params
```

> `--chdir` only takes effect alongside `--shell`; it folds into the command as `cd <dir> && <cmd>`.

---

## run

Trigger a build, optionally with parameters, optionally waiting for it to finish.

```bash
bee job run <name> [-p KEY=value ...] [--wait] [--timeout <seconds>]
```

| Option | Default | Description |
|---|---|---|
| `-p` / `--param KEY=value` | — | Build parameter (repeatable) |
| `--wait` | false | Block until the build finishes |
| `--timeout <s>` | 120 | Max seconds to wait (with `--wait`) |

```bash
bee job run build-api
bee job run deploy -p ENV=prod -p VERSION=1.2.3
bee job run build-api --wait --timeout 300       # wait up to 5 min, print result
```

---

## stop

Stop a running build.

```bash
bee job stop <name> <build_number>
```

```bash
bee job stop build-api 42
```

---

## log

Print a build's console log, or stream it live.

```bash
bee job log <name> [build_number] [-f|--follow]
```

- Omit `build_number` to use the last build.
- `-f` / `--follow` streams new output until the build completes (polls every 3s, only fetches new bytes).

```bash
bee job log build-api                # last build's full log
bee job log build-api 42             # build #42
bee job log build-api -f             # follow last build live
```

---

## status

Show recent build history.

```bash
bee job status <name> [--count <n>]
```

```bash
bee job status build-api             # last 10 builds
bee job status build-api --count 25
```

---

## copy / move

```bash
bee job copy <source> <destination>      # clone a job's config to a new name
bee job move <source> <folder>           # move a job to a folder ('.' = root)
```

```bash
bee job copy build-api build-api-clone
bee job move build-api team/backend       # → team/backend/build-api
bee job move team/backend/build-api .     # back to root
```

---

## delete

Delete one or more jobs or folders. Asks to confirm unless `--yes`.

```bash
bee job delete <name...> [--yes]
```

```bash
bee job delete old-job
bee job delete job1 job2 job3 --yes
```

If a job is already gone from the server, it's removed from local tracking anyway.

---

## track / untrack

```bash
bee job track <name...>      # add existing server jobs to your Mine list
bee job untrack <name...>    # remove from Mine (does NOT delete on server)
```

```bash
bee job track shared-build           # adopt a job someone else created
bee job untrack shared-build         # stop tracking it
```

---

## Folders Plus — controlled agents

If the CloudBees Folders Plus plugin is installed, you can restrict an agent to only run builds from approved folders. Three subcommands live under `bee job`:

```bash
bee job list-agents <folder>             # agents approved for a folder
bee job approve-agent <folder> <agent>   # run the full approval handshake
bee job remove-agent <folder> <agent> [--yes]   # revoke an agent
```

```bash
bee job approve-agent team MY_AGENT
bee job approve-agent team/backend MY_AGENT
bee job list-agents team
```

`approve-agent` runs the complete 5-step handshake (enable controlled mode on the agent, exchange request key/secret between folder and agent). If any step fails, partial artifacts are rolled back automatically.

> **Requires** admin permissions on both the agent and the folder, and the Folders Plus plugin installed. Not available on open-source Jenkins.

To toggle controlled-agent mode on an agent itself, use `bee node update <agent> --controlled-agent true|false`.

---

## Typical flows

### Create, run, watch

```bash
bee job create freestyle build-api --shell "make api" --node linux
bee job run build-api --wait
bee job log build-api               # inspect output
```

### Nightly job with failure email

```bash
bee job create freestyle nightly \
  --shell "make nightly" \
  --schedule "H 2 * * *" \
  --email "team@corp.com" --email-cond failed
```

### Organize into folders

```bash
bee job create folder team
bee job create folder backend --folder team
bee job move build-api team/backend
```
