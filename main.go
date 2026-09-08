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
	// Before dispatch, so every command sees the same settings -- including the
	// server the desktop icon spawns, which has no shell environment at all.
	loadConfig()
	switch os.Args[1] {
	case "serve":
		serve(os.Args[2:])
	case "setup":
		setup()
	case "open":
		open(os.Args[2:])
	case "config":
		config(os.Args[2:])
	case "install-desktop":
		installDesktop()
	case "uninstall-desktop":
		uninstallDesktop()
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
	fmt.Fprintln(os.Stderr, "  essaim-ui serve [--port N]      serve the UI (--no-auth for desktop use)")
	fmt.Fprintln(os.Stderr, "  essaim-ui open                  start it if needed and open the browser")
	fmt.Fprintln(os.Stderr, "  essaim-ui install-desktop       add a launcher icon (uninstall-desktop removes it)")
	fmt.Fprintln(os.Stderr, "  essaim-ui config [KEY=VALUE]    show or set settings the launcher can see")
	fmt.Fprint(os.Stderr, "  essaim-ui guide | help-json | version\n\n")
	fmt.Fprintln(os.Stderr, "  ESSAIM_URL   default http://127.0.0.1:8686")
	fmt.Fprintln(os.Stderr, "  BKN_URL      default http://127.0.0.1:7799")
	fmt.Fprintln(os.Stderr, "  ESSAIM_UI_DIR  default download directory offered to the browser")
}

// isLoopback is deliberately narrow: a name that merely RESOLVES to 127.0.0.1
// is not proof the socket is loopback-only, and "" or "0.0.0.0" binds every
// interface. Anything unrecognised is treated as remote.
func isLoopback(host string) bool {
	switch host {
	case "127.0.0.1", "::1", "localhost", "[::1]":
		return true
	}
	return false
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func clients() (*essaim.Client, *bknclient.Client) {
	return essaim.NewWithToken(env("ESSAIM_URL", "http://127.0.0.1:8686"), env("ESSAIM_TOKEN", "")),
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
	noAuth := false
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
		case "--no-auth":
			noAuth = true
		}
	}
	if env("ESSAIM_UI_NO_AUTH", "") != "" {
		noAuth = true
	}
	// Without the sign-in gate every caller can add a magnet, retarget a
	// download directory and delete files off the disk. That is an acceptable
	// trade on a machine you are sitting at, and an open remote-control API on
	// anything else -- so it is loopback only, with no override flag to get it
	// wrong with.
	if noAuth && !isLoopback(host) {
		fail(exitUsage, "unsafe_config",
			"--no-auth is refused on "+host+": it would expose an unauthenticated API that can add torrents and delete files",
			"serve on 127.0.0.1 (the default) for desktop use",
			"drop --no-auth and sign in with a bkn user to serve off loopback")
	}
	es, bkn := clients()
	srv := &web.Server{
		Essaim: es, Bkn: bkn,
		Dir:      env("ESSAIM_UI_DIR", "."),
		Insecure: insecure,
		NoAuth:   noAuth,
	}
	if noAuth {
		// Labels are per-user, so they still need an identity. One is optional:
		// with none, torrents work and the page reports labels_error, exactly
		// as it does when bkn is down.
		email, pass := env("ESSAIM_UI_EMAIL", ""), env("ESSAIM_UI_PASSWORD", "")
		if email != "" && pass != "" {
			t, err := bkn.Login(email, pass, env("ESSAIM_UI_ORG", ""))
			if err != nil {
				fmt.Fprintf(os.Stderr, "[serve] --no-auth: cannot sign in as %s (%v) -- labels are off, torrents work\n", email, err)
			} else {
				srv.SetLocal(web.Identity{Access: t.AccessToken, Refresh: t.RefreshToken, Email: t.User.Email})
				fmt.Fprintf(os.Stderr, "[serve] --no-auth: labels as %s\n", t.User.Email)
			}
		}
		if email == "" || pass == "" {
			// bkn is OPTIONAL for desktop use. Labels need an identity, so
			// without one there is nothing for bkn to do and no reason to
			// mention it -- let alone report it as down.
			srv.LabelsOff = true
			fmt.Fprintln(os.Stderr, "[serve] --no-auth: no identity set, so labels are off and bkn is not used")
			fmt.Fprintln(os.Stderr, "[serve] (torrents, speed caps, paths and removal all work without it)")
		}
	}
	addr := host + ":" + strconv.Itoa(port)
	fmt.Fprintf(os.Stderr, "[serve] essaim-ui %s on http://%s (essaim=%s bkn=%s)\n",
		Version, addr, env("ESSAIM_URL", "http://127.0.0.1:8686"), env("BKN_URL", "http://127.0.0.1:7799"))
	if noAuth {
		fmt.Fprintln(os.Stderr, "[serve] --no-auth: no sign-in, loopback only")
	}
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
		"requires": map[string]any{
			"essaim": "REQUIRED. There is nothing to show without it.",
			"bkn": "OPTIONAL. It provides identity, and labels are per-user state, so it is " +
				"needed only for labels or for the multi-user sign-in flow. With --no-auth and " +
				"no identity configured, bkn is not contacted at all and the page does not " +
				"mention it -- listing, adding, caps, paths and removal all work without it.",
		},
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
		"desktop": map[string]any{
			"what": "essaim-ui install-desktop puts a launcher icon in the menu that runs " +
				"`essaim-ui open`, which starts the server if nothing is answering and then " +
				"opens the browser. Clicking twice shows the app twice rather than failing on " +
				"a taken port.",
			"no_auth": "The launcher uses --no-auth: on your own machine the browser is the " +
				"window, and asking a person to authenticate to their own torrent client is " +
				"ceremony. It is REFUSED off loopback, with no override -- without the gate " +
				"every caller can add a magnet, retarget a directory and delete files.",
			"labels": "Labels are per-user, so --no-auth still needs an identity for them. Set " +
				"ESSAIM_UI_EMAIL and ESSAIM_UI_PASSWORD and the server signs in once at " +
				"startup; with neither, torrents work and the page reports labels_error, " +
				"exactly as it does when bkn is down.",
			"env": "A launcher inherits the DESKTOP session's environment, not your shell's, so " +
				"ESSAIM_URL/BKN_URL/ESSAIM_UI_EMAIL exported in ~/.bashrc never reach it -- the " +
				"icon starts on defaults and reports bkn unreachable at 127.0.0.1:7799, which " +
				"looks like a broken install. Use `essaim-ui config KEY=VALUE`, which writes " +
				"~/.config/essaim-ui/config.json (mode 600, known keys only). Precedence is " +
				"flags > environment > file > defaults.",
			"path": "The .desktop Exec pins the binary's path at install time. Re-run " +
				"install-desktop after moving or reinstalling it.",
		},
		"concepts": map[string]any{
			"label": "Your own note and tag on a torrent, keyed by infohash, stored in the bkn collection " +
				"essaimui/labels. The user_id is stamped by bkn from your token — a client cannot write " +
				"into somebody else's tenancy even by sending one.",
			"session": "A cookie holding your bkn access and refresh tokens. There is no server-side " +
				"session table and no user database here: bkn already is the identity store.",
		},
		"gotchas": []string{
			"bkn is optional. Before 0.3.1 an unconfigured bkn was reported as \"bkn down\" with a labels_error banner, so a perfectly working desktop install looked broken; now labels are simply off and bkn is not contacted.",
			"essaim 0.5.6 gates every daemon route but /_health behind its --token; before that only /_shutdown was checked, so a UI that sent nothing still worked against a daemon that had one. Set ESSAIM_TOKEN to match, or every call is 401.",
			"The UI shows torrents even when bkn is unreachable — it reports labels_error instead of failing the page. essaim not being reachable IS fatal for the list, because there is nothing to show.",
			"An expired access token is refreshed once, transparently. If the refresh also fails you are signed out rather than shown a stale error.",
			"Serving off loopback needs --secure-cookie and TLS in front, or the browser will send the session token in clear.",
			"--no-auth is loopback-only and there is no flag to override that. It is a desktop-app mode, not a deployment mode.",
			"A launcher entry inherits the desktop session's environment, not the shell you installed from.",
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
			"setup":             c(none, none),
			"serve":             c(none, []string{"--host <h>", "--port <n>", "--secure-cookie", "--no-auth"}),
			"open":              c(none, []string{"--port <n>", "--no-spawn"}),
			"config":            c([]string{"[KEY=VALUE ...]"}, none),
			"install-desktop":   c(none, none),
			"uninstall-desktop": c(none, none),
			"guide":             c(none, none),
			"help-json":         c(none, none),
			"version":           c(none, none),
		},
		"env": map[string]any{
			"ESSAIM_URL":         "essaim daemon base URL (default http://127.0.0.1:8686)",
			"BKN_URL":            "bkn base URL (default http://127.0.0.1:7799)",
			"BKN_ADMIN_TOKEN":    "setup only — declares the collection; never used while serving",
			"ESSAIM_UI_DIR":      "default download directory offered to the browser",
			"ESSAIM_UI_NO_AUTH":  "set to any value to imply --no-auth",
			"ESSAIM_UI_PORT":     "port used by `open` (default 8687)",
			"ESSAIM_UI_EMAIL":    "with ESSAIM_UI_PASSWORD, the bkn identity --no-auth uses for labels",
			"ESSAIM_UI_PASSWORD": "see ESSAIM_UI_EMAIL",
			"ESSAIM_UI_ORG":      "optional org for that identity",
		},
		"exit_codes": map[string]any{"0": "success", "78": "missing configuration", "80": "usage", "100": "unavailable"},
	}
}
