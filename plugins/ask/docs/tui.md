# TUI Guide

Launch with:

```bash
bee --ui
```

Requires an interactive terminal. Shares the same login, cache, and "Mine" list as the CLI — anything you create in the TUI is visible from `bee job list`, and vice versa.

---

## Layout

```
┌────────────────────────────────────────────────────────────────────┐
│ 🐝  1:Controllers  2:Nodes  3:Jobs ▾  4:Credentials  5:Info  alice@prod │  ← tab bar
├────────────────────────────────────────────────────────────────────┤
│ ⚙ Jobs  [MINE]  › /my-folder           ⟳ refreshing…                  │  ← screen header
│ / search…                                                           │  ← search bar (press /)
│    Status     T   Name             Build#   Description             │  ← column headers
│  ─────────────────────────────────────────────────────────────────│
│  ▶ ✓ OK       FS  build-api        #42      api service            │  ← cursor row
│    ✗ FAIL     FS  build-web        #17      web bundle             │
│    ◆          FD  my-folder/ ›     —        (Enter to drill in)    │  ← multi-selected (Space)
│                                    3/12  › more below              │
│  ┌─ build-api  #42 ──────────────────────────────────────────────┐ │  ← detail panel
│  │ type FS   schedule H 8 * * *   node linux                     │ │
│  └────────────────────────────────────────────────────────────────┘ │
│ ✓ Triggered: build-api                                              │  ← toast notification
├────────────────────────────────────────────────────────────────────┤
│ [Enter] menu  [^n] new  [^d] delete  [/] search  [r] refresh  …   │  ← footer key hints
└────────────────────────────────────────────────────────────────────┘
```

The layout scales automatically to your terminal width.

---

## Tabs

| # | Tab | What it shows |
|---|---|---|
| 1 | Controllers | All controllers on the server |
| 2 | Nodes | Agent nodes — status, executors, labels |
| 3 | Jobs | Jobs on the active controller; folder drill-down |
| 4 | Credentials | Credentials (system or user store) |
| 5 | Info | Health, version, plugin list, cache stats |
| 6 | FoldersPlus | Controlled-agent approval across agents and folders |

---

## Global Keys

These work from any tab, any time.

| Key | Action |
|---|---|
| `Tab` / `Shift+Tab` | Next / previous tab |
| `←` / `→` | Previous / next tab |
| `1` – `6` | Jump directly to tab number |
| `Ctrl+l` | Open login form (when not logged in) |
| `Ctrl+o` | Logout (asks to confirm) |
| `P` | Switch active profile (when more than one profile exists) |
| `L` | Toggle CLI-equivalent command log pane |
| `?` | Open help overlay |
| `Ctrl+q` | Quit |

---

## Navigation (all tables)

| Key | Action |
|---|---|
| `↑` / `↓` | Move cursor up / down one row |
| `Home` / `End` | Jump to first / last row |
| `Ctrl+f` / `Ctrl+b` | Page down / page up |
| `/` | Open search bar — filter rows as you type; `Esc` clears |
| `Space` | Toggle multi-select on cursor row |
| `Enter` | Open action menu for the cursor row |

---

## Common Per-Tab Keys

Available on the Controllers, Jobs, Nodes, Credentials, and FoldersPlus tabs.

| Key | Action |
|---|---|
| `Ctrl+n` | Create a new resource (Jobs, Nodes, Credentials) |
| `Ctrl+d` | Delete cursor row (or all multi-selected rows) |
| `Ctrl+a` | Toggle Mine / All view (remembered across restarts) |
| `F` | Toggle auto-refresh |
| `r` | Refresh immediately |

Edit, Track, and Untrack live inside the `Enter` action menu, not as standalone keys.

---

## Action Menu

Press `Enter` on any row to open its action menu. Inside the menu:

| Key | Action |
|---|---|
| `↑` / `↓` | Move selection |
| `1` – `9` | Pick an item directly by number |
| `Enter` | Run the selected action |
| `Esc` | Close menu, back to list |

```
list ──Enter──▶ Action Menu ──pick──▶ action
                    │                    ├─▶ Confirm dialog  (Delete, Stop, Logout)
                    │                    ├─▶ Form modal      (Edit, Run)
                    │                    └─▶ Log viewer      (View Log)
                    └──Esc──▶ list
```

---

## Jobs Tab

### Action menu items

| Item | What it does |
|---|---|
| View Log | Open log viewer for the last build |
| Run | Trigger a build (shows a parameter form if the job has String parameters) |
| Stop | Stop the running build |
| Edit | Edit the job configuration |
| Params | Manage String build parameters |
| Schedule | Set/clear the cron schedule |
| Email | Configure email notifications |
| Move | Move the job to a different folder |
| Delete | Delete the job |
| Controlled Agents | (Folder rows only) Manage folder–agent approvals |

Track and Untrack are available as bulk keys when rows are multi-selected (`i` / `u`), not in the single-row action menu.

### Folder navigation

| Key | Action |
|---|---|
| `Enter` (on a folder) | Drill into the folder |
| `Backspace` | Go up one folder level |

### Extra keys (Jobs only)

| Key | Action |
|---|---|
| `c` | Clone (copy) the cursor job |
| `m` | Move the cursor job to another folder |
| `A` | Open the Controlled Agents overlay (folder rows only) |

---

## Nodes Tab

### Action menu items

| Item | What it does |
|---|---|
| Toggle Offline | Take online / bring offline |
| Edit | Edit node configuration |
| Approve Folder | Approve this node to run a specific folder's builds |
| Track / Untrack | Add or remove from your Mine list |
| Delete | Delete the node |

---

## Credentials Tab

| Key | Action |
|---|---|
| `Shift+S` | Toggle between system store and user store (real refetch) |

### Action menu items

| Item | What it does |
|---|---|
| Edit | Update credential values |
| Track / Untrack | Add or remove from your Mine list |
| Delete | Delete the credential |

---

## Controllers Tab

| Key | Action |
|---|---|
| `Enter` | Open the controller info panel (system, current user, creation permissions) |
| `s` | Select — make the cursor controller the active one |
| `r` | Refresh the list |
| `F` | Toggle auto-refresh |
| `Esc` | Close the info panel |

The `*` in the list marks the currently active controller. Selecting a controller (`s`) scopes all subsequent Jobs / Nodes / Credentials operations to it.

---

## Info Tab

| Key | Action |
|---|---|
| `Ctrl+x` | Clear the entire local cache |

Shows: server health, version, number of nodes/jobs/credentials, installed CloudBees plugins, and local cache statistics.

---

## FoldersPlus Tab

Lists all agent nodes. For each agent you can approve a folder (run the controlled-agent handshake) or toggle controlled-agent mode — the same operations as `bee job approve-agent` and `bee node update --controlled-agent`.

> Requires the **CloudBees Folders Plus** plugin installed on the controller. Not available on open-source Jenkins.

| Key | Action |
|---|---|
| `a` | Approve a folder on the cursor agent (prompts for folder path) |
| `t` | Toggle controlled-agent mode on/off for the cursor agent (asks to confirm) |
| `r` | Refresh the agent list |
| `/` | Search / filter agents by name or label |
| `Esc` | Clear search |

---

## Multi-Select

Press `Space` to toggle selection on individual rows. Once any row is selected, bulk-action keys replace the single-row ones:

| Key | Action |
|---|---|
| `Space` | Toggle selection on cursor row |
| `Ctrl+d` | Delete all selected rows |
| `i` | Track all selected rows |
| `u` | Untrack all selected rows |
| `Esc` | Deselect all |

---

## Log Viewer

`Enter → View Log` on a job opens a live streaming log pane.

| Key | Action |
|---|---|
| `↑` / `↓` | Scroll one line |
| `Ctrl+f` / `Ctrl+b` | Page down / up |
| `Home` | Jump to top |
| `End` | Jump to bottom and re-pin to live tail |
| `[` | Switch to older build |
| `]` | Switch to newer build |
| `Esc` | Back to job list |

The log streams progressively — only new bytes are fetched, not the whole log each poll.

---

## Controlled Agents Overlay

Opened from: Jobs tab `A` key (folder rows) · Nodes action menu "Approve Folder".

| Key | Action |
|---|---|
| `↑` / `↓` | Move cursor |
| `a` | Approve a new folder–agent pair |
| `d` | Revoke approval (works on pending grants too) |
| `r` | Refresh the list |
| `Esc` | Close overlay |

---

## Command Log

`L` opens a pane at the bottom showing the CLI-equivalent command string for every action the TUI has performed this session. Useful for learning what commands to script.

---

## Forms and Modal Fields

When a form opens (create job, edit, etc.):

| Key | Action |
|---|---|
| `Tab` | Next field |
| `Shift+Tab` | Previous field |
| `Enter` | Submit (on last field or a submit button) |
| `Esc` | Cancel |

Fields that take a **filesystem path** (Remote Dir, Working Dir) support `Tab`-completion against your local filesystem.

---

## Display Options

### ASCII mode

If the TUI shows garbled characters, force ASCII symbols:

```bash
BEE_ASCII=1 bee --ui
```

### Terminal width

The layout adapts automatically. Minimum usable width is around 80 columns; wider terminals show more of long names and descriptions.
