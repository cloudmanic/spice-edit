---
title: "Custom Actions"
metaTitle: "Custom Actions in SpiceEdit — Shell Out from the Menu"
metaDescription: "Define shell-out actions in actions.json — SpiceEdit prepends them to the action menu. The killer use case: open a remote file on your local laptop."
summary: "Shell-out menu items defined in actions.json."
weight: 80
---

SpiceEdit reads user-defined shell-out actions from `~/.config/spiceedit/actions.json` and prepends them to the action menu. Each action runs against the currently open file when you click it.

The use case it was built for: you SSH from your laptop into a remote box, edit a file there, and want to _open it on your laptop_ — but Sixel and the Kitty graphics protocol don't survive the trip through tmux/zellij. The trick is to bypass the terminal entirely and pipe the file back over a second SSH connection.

## File location

`~/.config/spiceedit/actions.json` (or `$XDG_CONFIG_HOME/spiceedit/actions.json` when set). The file is optional — without it, the menu shows only built-in actions.

## Schema

```json
{
  "actions": [
    {
      "label": "Open on Rager",
      "command": "scp \"$FILE\" rager:~/Downloads/ && ssh rager open \"~/Downloads/$FILENAME\""
    },
    {
      "label": "Open on Cascade",
      "command": "scp \"$FILE\" cascade:~/Downloads/ && ssh cascade open \"~/Downloads/$FILENAME\""
    }
  ]
}
```

Each entry needs:

- **`label`** — the menu text. Keep it under 30 characters; long labels clip inside the modal.
- **`command`** — handed to `sh -c` with the editor-state env variables below exported.
- **`prompts`** *(optional)* — a list of input fields the editor collects before the command runs. See [Prompts](#prompts) below.

The full set of env vars available to every action's shell:

| Variable              | Value                                                            |
| --------------------- | ---------------------------------------------------------------- |
| `FILE`                | Absolute path of the active tab's file. Empty when no tab is open. |
| `FILENAME`            | Basename of `FILE`.                                              |
| `PROJECT_ROOT`        | Absolute path of the project root.                               |
| `ACTIVE_FOLDER`       | Absolute path of the sidebar's active folder (defaults to `PROJECT_ROOT`). |
| `ACTIVE_FOLDER_REL`   | `ACTIVE_FOLDER` relative to `PROJECT_ROOT` (empty when at root). |
| `CURRENT_FILE`        | Alias of `FILE`.                                                 |
| `CURRENT_FILE_REL`    | `FILE` relative to `PROJECT_ROOT` (empty when no tab open).      |

Prompt-less actions only enable when there's a file open — their command lines almost always reference `$FILE`. Actions with `prompts` stay enabled with no tab open, since they're typically pulling something *into* the project rather than acting on what's already there.

Commands run in a background goroutine, so a slow `scp` or hanging `ssh` won't freeze the editor. Success flashes in the status bar and forces an immediate sidebar refresh so a freshly-pulled file shows up without waiting on the auto-refresh tick. Failure opens an info modal with the captured `stderr` so the actual diagnostic is visible (the full output is also in `actions.log`).

## Prompts

Add a `prompts` array to an action and the editor opens a small form modal before running the command. Each prompt's value is exported as an env var named after its `key`. The headline use case is **Copy from remote** — pull a file from a known host into the active folder without leaving the editor:

```json
{
  "actions": [
    {
      "label": "Copy from remote",
      "prompts": [
        {
          "key": "HOST",
          "label": "Host",
          "type": "select",
          "options": ["cascade", "rager"]
        },
        {
          "key": "DEST_DIR",
          "label": "Local destination",
          "type": "text",
          "default": "${ACTIVE_FOLDER}"
        },
        {
          "key": "REMOTE_SRC",
          "label": "Remote file",
          "type": "text"
        }
      ],
      "command": "scp \"$HOST:$REMOTE_SRC\" \"$DEST_DIR/\""
    }
  ]
}
```

Each prompt needs:

- **`key`** — the env var name. Must match `[A-Z_][A-Z0-9_]*` so the shell can read it back as `$KEY` cleanly.
- **`label`** — the row label in the form modal.
- **`type`** — `"text"` for free-form input or `"select"` for a fixed option list.
- **`options`** — required for `"select"`, ignored otherwise.
- **`default`** — optional initial value. May reference any of the editor-state variables above using `${NAME}` syntax — those expand when the modal opens. Bare `$NAME` (no braces) is left alone so it's still readable by the shell later.

In the form modal: `Tab` / `Shift+Tab` move focus between fields, arrow keys cycle a focused select, `Enter` on the last field submits, `Esc` cancels. The mouse works too — click any field to focus it, click the `<` / `>` chevrons to cycle a select, click `[ Submit ]` or `[ Cancel ]` (or anywhere outside the modal).

## The two-hop SSH gotcha

`$HOME` and `~` outside `ssh "..."` quotes expand to the _SpiceEdit host's_ home directory — that's the remote box, not your laptop. To run something on your laptop, wrap the remote command in quotes:

```
ssh rager "open ~/Downloads/$FILENAME"
```

`$FILENAME` expands locally (you want that — it's a filename), but `~` is sent literally and rager's shell expands it on arrival.

## The "open on my laptop" workflow

Both example actions assume `rager` and `cascade` are SSH host aliases in the **remote** machine's `~/.ssh/config` that resolve back to your laptop. The setup, once:

1. **On your laptop**, generate (or pick) an SSH key pair you'll dedicate to inbound connections from your remote work box.
2. **On your laptop**, enable Remote Login (System Settings → General → Sharing → Remote Login on macOS) and add the public key to `~/.ssh/authorized_keys`.
3. **On the remote box**, drop the matching private key into `~/.ssh/id_<name>` and add a host alias:

   ```sshconfig
   Host rager
     HostName your-laptop.example.com   # or a Tailscale / mesh hostname
     User your-mac-username
     IdentityFile ~/.ssh/id_rager
   ```

4. Test it: `ssh rager echo hi` from the remote. Once that works, SpiceEdit can drive it.

If your laptop sits behind NAT, point `HostName` at a Tailscale, WireGuard, or Cloudflare-tunnel address — anywhere the remote can reach the laptop directly. The action itself is `scp` plus `ssh`; it doesn't care how the network gets there.

## Other patterns

The schema is deliberately small. Anything `sh` can do, `actions.json` can do:

```json
{
  "actions": [
    {
      "label": "Send to ChatGPT",
      "command": "cat \"$FILE\" | pbcopy && open https://chat.openai.com/"
    },
    {
      "label": "Lint with eslint",
      "command": "cd $(dirname \"$FILE\") && eslint \"$FILENAME\""
    },
    { "label": "Run formatter", "command": "gofmt -w \"$FILE\"" },
    { "label": "Open in Finder", "command": "open -R \"$FILE\"" },
    { "label": "Copy to gist", "command": "gh gist create \"$FILE\"" }
  ]
}
```

## Debug log

Every custom-action invocation appends a record to `~/.local/state/spiceedit/actions.log` (or `$XDG_STATE_HOME/spiceedit/actions.log` when set). One entry per run, human-readable, with the exact command, the env vars exported, the duration, and the combined stdout / stderr:

```
[2026-04-30T13:26:32-07:00] Open on Rager (1.234s) → ok
  command: scp "$FILE" rager:~/Downloads/ && ssh rager open "$HOME/Downloads/$FILENAME"
  FILE:     /Users/spicer/dev/foo/bar.txt
  FILENAME: bar.txt
  --- output ---
  --- end ---

[2026-04-30T13:27:01-07:00] Open on Cascade (0.521s) → exit status 1
  command: scp "$FILE" cascade:~/Downloads/ && ssh cascade open "$HOME/Downloads/$FILENAME"
  FILE:     /Users/spicer/dev/foo/bar.txt
  FILENAME: bar.txt
  --- output ---
  ssh: connect to host cascade port 22: Connection refused
  lost connection
  --- end ---
```

Run `tail -f ~/.local/state/spiceedit/actions.log` while you click around to watch entries roll in. There's no rotation — each entry is one line plus a few lines of output, so the file grows slowly. Delete it whenever you want to start fresh.
