# essaim-ui

A web UI for [essaim](https://github.com/javimosch/essaim), the agent-first
BitTorrent client — with per-user state in [bkn](https://github.com/javimosch/bkn).

One Go binary, one embedded page, no build step and no CDN.

```sh
essaim daemon start                       # essaim owns the torrents
export BKN_ADMIN_TOKEN=...                # only for the next line
essaim-ui setup                           # declare the bkn collection + policy
essaim-ui serve                           # http://127.0.0.1:8687
```

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

`serve` binds loopback. Off loopback, pass `--secure-cookie` and put TLS in
front, or the browser will send the session token in clear.

## Agent-first

`essaim-ui guide` and `essaim-ui help-json` are embedded in the binary, per the
[spec family](https://cli-specs.intrane.fr): stdout is data, stderr is context,
exit codes are semantic.

## Licence

MIT.
