// Command essaim-ui is a web UI for the essaim BitTorrent daemon, with its
// per-user state in bkn.
//
// The split is deliberate. essaim owns torrents and re-derives progress from
// the files on disk. bkn owns identity and the user's own labels, and enforces
// who may read them through a collection access policy — so this process never
// holds an admin token while serving, and cannot leak one user's data to
// another even if a handler forgets a check.
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/javimosch/essaim-ui/internal/bknclient"
	"github.com/javimosch/essaim-ui/internal/essaim"
	"github.com/javimosch/essaim-ui/internal/web"
)

var Version = "0.0.0-dev"

const (
	exitUsage       = 80
	exitConfig      = 78
	exitUnavailable = 100
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(exitUsage)
	}
	switch os.Args[1] {
	case "serve":
		serve(os.Args[2:])
	case "setup":
		setup()
	case "guide":
		out(guide())
	case "help-json":
		out(helpJSON())
	case "version", "--version", "-v":
		out(map[string]any{"ok": true, "tool": "essaim-ui", "tool_version": Version, "version": "1.0"})
	case "help", "--help", "-h":
		usage()
	default:
		fail(exitUsage, "unknown_command", fmt.Sprintf("unknown command %q", os.Args[1]), "essaim-ui help-json")
	}
}

func out(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func fail(code int, typ, msg string, suggestions ...string) {
	out(map[string]any{"ok": false, "error": map[string]any{
		"code": code, "type": typ, "message": msg, "recoverable": false, "suggestions": suggestions,
	}})
	os.Exit(code)
}

func usage() {
	fmt.Fprint(os.Stderr, "essaim-ui — a web UI for the essaim daemon, with per-user state in bkn\n\n")
	fmt.Fprintln(os.Stderr, "  essaim-ui setup                 declare the bkn collection + access policy (needs BKN_ADMIN_TOKEN)")
	fmt.Fprintln(os.Stderr, "  essaim-ui serve [--port N]      serve the UI")
	fmt.Fprint(os.Stderr, "  essaim-ui guide | help-json | version\n\n")
	fmt.Fprintln(os.Stderr, "  ESSAIM_URL   default http://127.0.0.1:8686")
	fmt.Fprintln(os.Stderr, "  BKN_URL      default http://127.0.0.1:7799")
	fmt.Fprintln(os.Stderr, "  ESSAIM_UI_DIR  default download directory offered to the browser")
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func clients() (*essaim.Client, *bknclient.Client) {
	return essaim.New(env("ESSAIM_URL", "http://127.0.0.1:8686")),
		bknclient.New(env("BKN_URL", "http://127.0.0.1:7799"))
}

// setup is separate from serve on purpose: a server that can rewrite its own
// authorization rules at boot is a server whose rules mean less.
func setup() {
	admin := os.Getenv("BKN_ADMIN_TOKEN")
	if admin == "" {
		fail(exitConfig, "missing_config", "BKN_ADMIN_TOKEN is required to declare the collection",
			"export BKN_ADMIN_TOKEN=... && essaim-ui setup")
	}
	_, bkn := clients()
	if err := bkn.Setup(admin); err != nil {
		fail(exitUnavailable, "setup_failed", err.Error())
	}
	out(map[string]any{"ok": true, "collection": bknclient.LabelsRef,
		"access": "read/create/update/delete = owner, owner_field = user_id"})
}

func serve(args []string) {
	host, port := "127.0.0.1", 8687
	insecure := true // loopback by default, so the cookie must work over http
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--port":
			if i+1 < len(args) {
				i++
				n, err := strconv.Atoi(args[i])
				if err != nil {
					fail(exitUsage, "invalid_value", "--port must be a number")
				}
				port = n
			}
		case "--host":
			if i+1 < len(args) {
				i++
				host = args[i]
			}
		case "--secure-cookie":
			insecure = false
		}
	}
	es, bkn := clients()
	srv := &web.Server{
		Essaim: es, Bkn: bkn,
		Dir:      env("ESSAIM_UI_DIR", "."),
		Insecure: insecure,
	}
	addr := host + ":" + strconv.Itoa(port)
	fmt.Fprintf(os.Stderr, "[serve] essaim-ui %s on http://%s (essaim=%s bkn=%s)\n",
		Version, addr, env("ESSAIM_URL", "http://127.0.0.1:8686"), env("BKN_URL", "http://127.0.0.1:7799"))
	if !insecure && host == "127.0.0.1" {
		fmt.Fprintln(os.Stderr, "[serve] --secure-cookie on loopback: the browser will not send the session over http")
	}
	h := &http.Server{Addr: addr, Handler: srv.Routes(), ReadHeaderTimeout: 10 * time.Second}
	if err := h.ListenAndServe(); err != nil {
		fail(exitUnavailable, "listen_failed", err.Error())
	}
}

func guide() map[string]any {
	return map[string]any{
		"essaim-ui": Version,
		"one_liner": "A web UI for the essaim BitTorrent daemon, with per-user state in bkn.",
		"model": map[string]any{
			"split": "essaim owns torrents and re-derives progress from the files on disk. " +
				"bkn owns identity and the user's own labels.",
			"why_bkn_enforces": "Every per-user call carries the USER's bkn access token, never an admin " +
				"token. bkn's collection access policy decides what that user can see, so ownership is " +
				"enforced by the store rather than by this process remembering to filter.",
			"admin_token": "Used by `setup` only, to declare the collection. Never on the serving path.",
		},
		"loop": []string{
			"essaim-ui setup            (once, with BKN_ADMIN_TOKEN)",
			"essaim daemon start        (essaim owns the torrents)",
			"essaim-ui serve            (open the page, sign in with a bkn user)",
		},
		"concepts": map[string]any{
			"label": "Your own note and tag on a torrent, keyed by infohash, stored in the bkn collection " +
				"essaimui/labels. The user_id is stamped by bkn from your token — a client cannot write " +
				"into somebody else's tenancy even by sending one.",
			"session": "A cookie holding your bkn access and refresh tokens. There is no server-side " +
				"session table and no user database here: bkn already is the identity store.",
		},
		"gotchas": []string{
			"The UI shows torrents even when bkn is unreachable — it reports labels_error instead of failing the page. essaim not being reachable IS fatal for the list, because there is nothing to show.",
			"An expired access token is refreshed once, transparently. If the refresh also fails you are signed out rather than shown a stale error.",
			"Serving off loopback needs --secure-cookie and TLS in front, or the browser will send the session token in clear.",
		},
		"see_also": []string{
			"https://github.com/javimosch/essaim",
			"https://github.com/javimosch/bkn",
		},
	}
}

func helpJSON() map[string]any {
	none := []string{}
	c := func(a, f []string) map[string]any { return map[string]any{"args": a, "flags": f} }
	return map[string]any{
		"tool":    "essaim-ui",
		"version": Version,
		"commands": map[string]any{
			"setup":     c(none, none),
			"serve":     c(none, []string{"--host <h>", "--port <n>", "--secure-cookie"}),
			"guide":     c(none, none),
			"help-json": c(none, none),
			"version":   c(none, none),
		},
		"env": map[string]any{
			"ESSAIM_URL":      "essaim daemon base URL (default http://127.0.0.1:8686)",
			"BKN_URL":         "bkn base URL (default http://127.0.0.1:7799)",
			"BKN_ADMIN_TOKEN": "setup only — declares the collection; never used while serving",
			"ESSAIM_UI_DIR":   "default download directory offered to the browser",
		},
		"exit_codes": map[string]any{"0": "success", "78": "missing configuration", "80": "usage", "100": "unavailable"},
	}
}
