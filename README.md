# essaim-ui

A web UI for [essaim](https://github.com/javimosch/essaim), the agent-first
BitTorrent client — with per-user state in [bkn](https://github.com/javimosch/bkn).

One static Go binary with the page embedded — no build step, no CDN, no
node_modules. Deploying it is a copy.

bkn is **optional**: for desktop use (`--no-auth`) essaim alone is enough, and
bkn is what adds per-user labels. See [Do you need bkn?](#do-you-need-bkn).

## Install

```sh
curl -fsSL -o essaim-ui https://github.com/javimosch/essaim-ui/releases/latest/download/essaim-ui-linux-amd64
chmod +x essaim-ui && sudo mv essaim-ui /usr/local/bin/    # or ~/.local/bin, no root needed
```

Verify it if you like — the release carries a `.sha256` beside the binary.

## Run

```sh
essaim daemon start                       # essaim owns the torrents
export BKN_ADMIN_TOKEN=...                # only for the next line
essaim-ui setup                           # declare the bkn collection + policy
essaim-ui serve                           # http://127.0.0.1:8687
```

You need three things running: **essaim** (the torrents), **bkn** (identity and
your labels), and this. essaim-ui tells you which of them it cannot reach:

```sh
curl -s localhost:8687/_health
{"ok":true,"service":"essaim-ui","essaim":{"ok":true,"version":"0.5.1"},"bkn":{"ok":true}}
```

## If your daemon has a token

essaim 0.5.6 gates every daemon route but `/_health` behind `--token`; before
that only `/_shutdown` was checked, so a UI that sent nothing still worked. Give
essaim-ui the same token or every call comes back 401:

```sh
essaim-ui config ESSAIM_TOKEN=<the daemon's --token>
```

A daemon on loopback with no token needs none of this — that is the default, and
the desktop case.

## Do you need bkn?

**For desktop use, no.** With `--no-auth`, essaim-ui runs against the essaim
daemon alone: listing, adding by magnet, speed caps, seed and ratio controls,
the path on disk and remove-with-data all work with no bkn anywhere. It is not
contacted, and the page does not mention it.

bkn is what you add when you want **labels** — your own note and tag per
torrent. Those are per-user state, so they need an identity, which is the thing
bkn provides:

```sh
essaim-ui config ESSAIM_UI_EMAIL=you@example.com ESSAIM_UI_PASSWORD=...
```

You also need bkn if you want the **sign-in** flow instead of `--no-auth` — for
example serving the UI to more than one person, where each sees only their own
labels. That is what the access policy in `setup` is for.

| you want | essaim | bkn |
|---|---|---|
| a torrent client with a browser UI | yes | — |
| …plus your own notes and tags | yes | yes |
| …plus separate users, each with their own | yes | yes |

## As a desktop app

On your own machine the browser is the window, and signing in to your own
torrent client is ceremony rather than security. `install-desktop` puts an icon
in the launcher:

```sh
essaim-ui install-desktop     # icon + .desktop entry under ~/.local/share
essaim-ui uninstall-desktop   # removes both
```

The icon runs `essaim-ui open`, which starts the server if nothing is answering
and then opens the browser — so clicking it twice shows the app twice instead of
failing on a taken port. You can run it by hand too:

```sh
essaim-ui open                # start if needed, then open the browser
essaim-ui open --no-spawn     # only open; fail if nothing is serving
essaim-ui serve --no-auth     # the mode the launcher uses
```

**`--no-auth` is loopback-only, and there is no flag to override that.** Without
the sign-in gate, every caller can add a magnet, retarget a download directory
and delete files off the disk; that is a fair trade on a machine you are sitting
at and an open remote-control API on anything else. Asking for it on any other
address is refused.

Labels are per-user, so they still need an identity. Set `ESSAIM_UI_EMAIL` and
`ESSAIM_UI_PASSWORD` and the server signs in once at startup; with neither, the
torrents work and the page reports `labels_error` — the same way it behaves when
bkn is simply down.

### Configure it where the launcher can see it

A `.desktop` entry does not inherit your shell, so `ESSAIM_URL` exported in
`~/.bashrc` never reaches the icon — it starts on the defaults and reports
`bkn unreachable at 127.0.0.1:7799`, which looks like a broken install. Put
settings in the config file instead:

```sh
essaim-ui config BKN_URL=http://127.0.0.1:7799 ESSAIM_UI_EMAIL=you@example.com
essaim-ui config                      # show what is set (passwords redacted)
essaim-ui config ESSAIM_UI_ORG=       # empty value removes a key
```

It lives at `~/.config/essaim-ui/config.json`, mode `600` because it can hold a
password, and only the known keys are read from it. Precedence is
**flags > environment > this file > defaults**, so nothing in the file can
override a choice you made on the command line.

One more thing: `Exec` pins the binary's path at install time. Re-run
`install-desktop` after moving or reinstalling it.

## The split, and why it matters

```
browser ── essaim-ui ── essaim daemon      torrents, progress re-derived from disk
                     └─ bkn                identity, and your own labels
```

**essaim-ui never holds an admin token while serving.** Every per-user call
carries the *user's* bkn access token, so bkn's collection access policy
decides what that user can see. Ownership is enforced by the store, not by this
process remembering to filter — which means a forgotten check in a handler
cannot leak one user's labels to another, because the query it sends is already
scoped by the token it sent.

The admin token appears in exactly one place, `essaim-ui setup`, which declares:

```
essaimui/labels   owner_field=user_id   read/create/update/delete = owner
```

A server that can rewrite its own authorization rules at boot is a server whose
rules mean less, so that is a separate command rather than something `serve`
does.

Verified end to end: a client that POSTs `"user_id":"SPOOFED"` gets its own id
stored instead, because a scoped create stamps tenancy from the token. Two
users, one shared torrent, and neither sees the other's note.

## What the UI can and cannot do

Set when you add a torrent:

- **the full download path** — typed in, or defaulted from `ESSAIM_UI_DIR`
- **seed on/off**, **upload and download caps in KB/s**, and a **stop-at ratio**
- a private note

Shown per torrent: the **full path on disk**, state, pieces, bytes, peers, seed
status and cap, upload total, and any error the daemon reports.

Removing offers two actions, because they are not the same decision:

- **remove** — drops the entry, leaves the bytes (this is all essaim's own
  `DELETE` does)
- **remove + data** — also deletes `dir/name`, after a confirmation that names
  the exact path. This is the only destructive thing essaim-ui does, so the
  guards are deliberately paranoid: the name must be a single path element, the
  target must resolve inside the directory, and a torrent whose metadata has
  not resolved yet is refused outright rather than falling back to deleting the
  directory itself.

Every torrent row also carries live controls — **seed on/off**, the **upload
cap**, and a **stop-at ratio** — applied through `PATCH /torrents/{id}`. Only
the fields you changed are sent, so adjusting a cap cannot silently reset a
ratio. The daemon speaks percent (it has no floats); the UI speaks ratios and
converts at the edge, so you type `0.5` and it sends `50`.

Both of those needed essaim to grow the capability first — a `PATCH` route and
a ratio concept — rather than being faked here by a process second-guessing the
daemon that owns the torrents. They landed in essaim alongside `essaim set`.

The **download cap** is there too, on the add form and per row. It is the one
control that is not instant: essaim applies it when the torrent next starts,
because its pacer takes its interval at job start and restarting a running job
from the control loop would hang essaim's supervisor. The row says
`saved · applies on next start` rather than implying otherwise.

## Degrading honestly

Torrents come from essaim and labels from bkn, so the two fail differently and
the UI says which:

- **bkn unreachable** — the torrent list still renders, with `labels_error`
  explaining why the notes are missing. Tested by stopping bkn mid-session.
- **essaim unreachable** — that *is* fatal for the list, because there is
  nothing left to show.

An expired access token is refreshed once, transparently. If the refresh also
fails you are signed out rather than shown a stale error.

## Configuration

| | |
|---|---|
| `ESSAIM_URL` | essaim daemon (default `http://127.0.0.1:8686`) |
| `BKN_URL` | bkn (default `http://127.0.0.1:7799`) |
| `BKN_ADMIN_TOKEN` | `setup` only — never read while serving |
| `ESSAIM_UI_DIR` | default download directory offered to the browser |
| `ESSAIM_UI_NO_AUTH` | set to anything to imply `--no-auth` |
| `ESSAIM_UI_PORT` | port used by `open` (default `8687`) |
| `ESSAIM_UI_EMAIL` / `ESSAIM_UI_PASSWORD` | the bkn identity `--no-auth` uses for labels |
| `ESSAIM_UI_ORG` | optional org for that identity |
| `ESSAIM_TOKEN` | the essaim daemon's `--token`, if it has one |

`serve` binds loopback. Off loopback, pass `--secure-cookie` and put TLS in
front, or the browser will send the session token in clear.

## Agent-first

`essaim-ui guide` and `essaim-ui help-json` are embedded in the binary, per the
[spec family](https://cli-specs.intrane.fr): stdout is data, stderr is context,
exit codes are semantic.

## Licence

MIT.
