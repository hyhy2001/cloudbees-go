# bee node — Agent Nodes

Manage build agent nodes on the active CloudBees controller. Supports SSH agents (auto-connect via SSH) and JNLP/Inbound agents (agent connects back to the controller). Requires a controller to be selected first.

---

## list

```bash
bee node list [--all]
```

| Option | Default | Description |
|---|---|---|
| `--all` | false | Show all nodes (default: Mine list only) |

```bash
bee node list              # your tracked nodes
bee node list --all        # everything on the controller
```

---

## get

Show detailed information about one node.

```bash
bee node get <name>
```

Displays name, offline status, executor count, labels, launcher type, remote directory, and description.

---

## create

Create a Permanent Agent. Pass `--host` for SSH; omit it for JNLP/Inbound.

```bash
bee node create <name> \
  --remote-dir <path> \
  [--executors <n>] \
  [--labels "<space-separated>"] \
  [--description <text>] \
  [--host <ssh_host>] \
  [--port 22] \
  [--cred-id <credential_id>] \
  [--java-path <path>] \
  [--availability always|demand] \
  [--in-demand-delay <minutes>] \
  [--idle-delay <minutes>]
```

| Option | Default | Description |
|---|---|---|
| `--remote-dir <path>` | **required** | Work directory on the agent, e.g. `/home/jenkins` |
| `--executors <n>` | `1` | Parallel build slots |
| `--labels <labels>` | `""` | Space-separated labels (for job `--node` targeting) |
| `--description <text>` | `""` | Free-text description |
| `--host <host>` | — | SSH host; **omit** for JNLP/Inbound agent |
| `--port <n>` | `22` | SSH port |
| `--cred-id <id>` | — | Credential ID for SSH authentication |
| `--java-path <path>` | auto-detect | Path to Java on the agent (SSH only) |
| `--availability` | `always` | Retention strategy: `always` or `demand` |
| `--in-demand-delay <min>` | `0` | Minutes with work queued before going online (`demand`) |
| `--idle-delay <min>` | `1` | Minutes idle before going offline (`demand`) |

### SSH agent

```bash
bee node create linux-build \
  --remote-dir /home/jenkins \
  --host 10.0.0.10 \
  --cred-id ssh-key \
  --labels "linux docker" \
  --executors 4
```

The agent auto-connects as soon as it's created (CloudBees makes the outgoing SSH connection).

If `--cred-id` is omitted, a warning is printed — ensure key-based SSH is configured on the agent.

### JNLP/Inbound agent

```bash
bee node create win-build \
  --remote-dir C:/jenkins \
  --executors 2 \
  --labels "windows dotnet"
```

No `--host`. After creation, go to `Manage Jenkins → Nodes → win-build → Agent command` to get the command to run on the agent machine.

### On-demand agent

```bash
bee node create ephemeral \
  --remote-dir /home/jenkins \
  --host 10.0.0.50 --cred-id ssh-key \
  --availability demand \
  --in-demand-delay 1 \
  --idle-delay 5
```

---

## update

Partial update — only the flags you pass change. Everything else stays as-is.

```bash
bee node update <name> \
  [--description <text>] \
  [--remote-dir <path>] \
  [--executors <n>] \
  [--labels "<labels>"] \
  [--launcher ssh|jnlp] \
  [--host <ssh_host>] \
  [--port <n>] \
  [--cred-id <credential_id>] \
  [--java-path <path>] \
  [--availability always|demand] \
  [--in-demand-delay <min>] \
  [--idle-delay <min>] \
  [--controlled-agent true|false]
```

`--launcher` switches the launch method between `ssh` and `jnlp` without recreating the node.

`--controlled-agent true|false` toggles **CloudBees Folders Plus controlled-agent mode** — when `true`, the agent only accepts builds from approved folders.

```bash
bee node update linux-build --executors 8
bee node update linux-build --labels "linux docker k8s"
bee node update win-build --launcher ssh --host 10.0.0.30 --cred-id win-key
bee node update linux-build --controlled-agent true      # Folders Plus
```

---

## copy

Clone an existing node's configuration to a new name.

```bash
bee node copy <source_name> <new_name>
```

```bash
bee node copy linux-build linux-build-2
```

Copies the full config.xml — launcher type, labels, executors, remote dir, everything. The new node is added to your Mine list.

---

## offline / online

Mark a node offline or bring it back.

```bash
bee node offline <name> [--reason <message>]
bee node online <name>
```

```bash
bee node offline linux-build --reason "Scheduled maintenance"
bee node online linux-build
```

Both commands are idempotent — offline on an already-offline node prints an info message and exits clean.

---

## delete

Delete one or more nodes. Asks to confirm unless `--yes`.

```bash
bee node delete <name...> [--yes]
```

```bash
bee node delete old-agent
bee node delete agent1 agent2 --yes
```

---

## track / untrack

```bash
bee node track <name...>      # add existing server nodes to your Mine list
bee node untrack <name...>    # remove from Mine (does NOT delete on server)
```

```bash
bee node track shared-linux           # adopt a node someone else created
bee node untrack shared-linux
```

---

## Availability modes

| Mode | Behaviour | Tune with |
|---|---|---|
| `always` | Stay online as much as possible | — |
| `demand` | Come online when there's demand; go offline when idle | `--in-demand-delay`, `--idle-delay` |

`--in-demand-delay` — minutes with a build queued before the agent connects (default `0`: connect immediately).
`--idle-delay` — minutes idle with no builds before the agent disconnects (default `1`).

---

## Typical flows

### Add an SSH build agent

```bash
# First, create the SSH credential
bee cred create --id ssh-linux --username jenkins --description "Linux build agent key"
# (password prompted — use the private key passphrase, or leave empty for keyless)

# Then create the node
bee node create linux-builder \
  --remote-dir /home/jenkins \
  --host 10.0.0.100 \
  --cred-id ssh-linux \
  --labels "linux docker" \
  --executors 4
```

### Scale out by cloning

```bash
bee node copy linux-builder linux-builder-2
bee node update linux-builder-2 --host 10.0.0.101
```

### Maintenance rotation

```bash
bee node offline linux-builder --reason "Kernel upgrade"
# ... do work on the machine ...
bee node online linux-builder
```
