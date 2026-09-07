// Package web is the HTTP layer: a small JSON API for the browser plus the
// page itself.
//
// Sessions are a cookie holding bkn's access and refresh tokens. essaim-ui
// keeps no user database and no server-side session table — bkn already is
// the identity store, and a second copy would only be a second thing to get
// out of sync.
package web

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/javimosch/essaim-ui/internal/bknclient"
	"github.com/javimosch/essaim-ui/internal/essaim"
)

const sessionCookie = "essaim_ui"

type Server struct {
	Essaim   *essaim.Client
	Bkn      *bknclient.Client
	Dir      string // default download directory offered to the browser
	Insecure bool   // allow the session cookie over plain http (local testing)
}

type session struct {
	Access  string `json:"a"`
	Refresh string `json:"r"`
	Email   string `json:"e"`
}

func (s *Server) setSession(w http.ResponseWriter, sess session) {
	b, _ := json.Marshal(sess)
	http.SetCookie(w, &http.Cookie{
		Name:  sessionCookie,
		Value: base64.RawURLEncoding.EncodeToString(b),
		Path:  "/",
		// HttpOnly: the page never needs to read the token, and script access
		// is the difference between an XSS bug and an account takeover.
		HttpOnly: true,
		Secure:   !s.Insecure,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(30 * 24 * time.Hour),
	})
}

func (s *Server) session(r *http.Request) (session, bool) {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return session{}, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(c.Value)
	if err != nil {
		return session{}, false
	}
	var sess session
	if err := json.Unmarshal(raw, &sess); err != nil || sess.Access == "" {
		return session{}, false
	}
	return sess, true
}

func (s *Server) clearSession(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func fail(w http.ResponseWriter, status int, typ, msg string) {
	writeJSON(w, status, map[string]any{"ok": false, "error": map[string]any{"type": typ, "message": msg}})
}

// authed runs next with a live access token, refreshing once if bkn says the
// access token has expired. A silent refresh is the difference between a
// 15-minute session and a usable one.
func (s *Server) authed(next func(http.ResponseWriter, *http.Request, session)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sess, ok := s.session(r)
		if !ok {
			fail(w, http.StatusUnauthorized, "not_authenticated", "sign in first")
			return
		}
		next(w, r, sess)
	}
}

// withRefresh retries an operation once after refreshing, so an expired access
// token is invisible to the user rather than a random logout.
func (s *Server) withRefresh(w http.ResponseWriter, sess session, op func(token string) error) error {
	err := op(sess.Access)
	if err == nil || !errors.Is(err, bknclient.ErrUnauthenticated) || sess.Refresh == "" {
		return err
	}
	t, rerr := s.Bkn.Refresh(sess.Refresh)
	if rerr != nil {
		return err // report the original failure, not the refresh's
	}
	sess.Access, sess.Refresh = t.AccessToken, t.RefreshToken
	s.setSession(w, sess)
	return op(sess.Access)
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /_health", func(w http.ResponseWriter, r *http.Request) {
		out := map[string]any{"ok": true, "service": "essaim-ui"}
		if h, err := s.Essaim.Health(); err == nil {
			out["essaim"] = map[string]any{"ok": true, "version": h.Version, "jobs": h.Jobs}
		} else {
			out["essaim"] = map[string]any{"ok": false, "error": err.Error()}
		}
		out["bkn"] = map[string]any{"ok": s.Bkn.Health() == nil}
		writeJSON(w, http.StatusOK, out)
	})

	mux.HandleFunc("POST /api/login", func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Email, Password, Org string }
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
			fail(w, http.StatusBadRequest, "validation_error", "body must be JSON")
			return
		}
		t, err := s.Bkn.Login(body.Email, body.Password, body.Org)
		if err != nil {
			fail(w, http.StatusUnauthorized, "bad_credentials", err.Error())
			return
		}
		s.setSession(w, session{Access: t.AccessToken, Refresh: t.RefreshToken, Email: t.User.Email})
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "email": t.User.Email})
	})

	mux.HandleFunc("POST /api/logout", func(w http.ResponseWriter, r *http.Request) {
		s.clearSession(w)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})

	mux.HandleFunc("GET /api/me", func(w http.ResponseWriter, r *http.Request) {
		sess, ok := s.session(r)
		if !ok {
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "signed_in": false})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "signed_in": true, "email": sess.Email, "dir": s.Dir})
	})

	// Torrents come from essaim; labels come from bkn scoped to the caller.
	// They are joined here rather than in the browser so a page load is one
	// request, and so an unlabelled torrent still renders.
	mux.HandleFunc("GET /api/torrents", s.authed(func(w http.ResponseWriter, r *http.Request, sess session) {
		ts, err := s.Essaim.List()
		if err != nil {
			fail(w, http.StatusBadGateway, "essaim_unreachable", err.Error())
			return
		}
		var labels []bknclient.Label
		lerr := s.withRefresh(w, sess, func(tok string) error {
			var e error
			labels, e = s.Bkn.Labels(tok)
			return e
		})
		byHash := map[string]bknclient.Label{}
		for _, l := range labels {
			byHash[l.Infohash] = l
		}
		out := make([]map[string]any, 0, len(ts))
		for _, t := range ts {
			row := map[string]any{"torrent": t, "path": fullPath(t.Dir, t.Name)}
			if l, ok := byHash[t.Infohash]; ok && t.Infohash != "" {
				row["label"] = l
			}
			out = append(out, row)
		}
		res := map[string]any{"ok": true, "rows": out}
		if lerr != nil {
			// The torrents are still worth showing; say why the labels are not.
			res["labels_error"] = lerr.Error()
		}
		writeJSON(w, http.StatusOK, res)
	}))

	mux.HandleFunc("POST /api/torrents", s.authed(func(w http.ResponseWriter, r *http.Request, sess session) {
		var body struct {
			Source    string `json:"source"`
			Dir       string `json:"dir"`
			Seed      bool   `json:"seed"`
			UpLimit   int    `json:"up_limit"`
			DownLimit int    `json:"down_limit"`
			RatioPct  int    `json:"ratio_limit_pct"`
			Note      string `json:"note"`
			Tag       string `json:"tag"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
			fail(w, http.StatusBadRequest, "validation_error", "body must be JSON")
			return
		}
		if strings.TrimSpace(body.Source) == "" {
			fail(w, http.StatusBadRequest, "validation_error", "source is required (magnet, .torrent path or https URL)")
			return
		}
		dir := body.Dir
		if dir == "" {
			dir = s.Dir
		}
		t, err := s.Essaim.Add(body.Source, dir, body.Seed, body.UpLimit, body.DownLimit, body.RatioPct)
		if err != nil {
			fail(w, http.StatusBadGateway, "essaim_error", err.Error())
			return
		}
		if body.Note != "" || body.Tag != "" {
			// Best-effort: a torrent that downloads but whose note failed to
			// save is a much better outcome than refusing the download.
			_ = s.withRefresh(w, sess, func(tok string) error {
				_, e := s.Bkn.PutLabel(tok, bknclient.Label{
					Infohash: t.Infohash, Source: t.Source, Name: t.Name,
					Note: body.Note, Tag: body.Tag,
				})
				return e
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "torrent": t})
	}))

	// ?data=1 also deletes the downloaded bytes. essaim's own DELETE never
	// touches the filesystem, so this is the one destructive thing essaim-ui
	// does and it is opt-in per request rather than a setting somebody forgets.
	mux.HandleFunc("DELETE /api/torrents/{id}", s.authed(func(w http.ResponseWriter, r *http.Request, sess session) {
		id := r.PathValue("id")
		withData := r.URL.Query().Get("data") == "1"

		var dir, name string
		if withData {
			// Resolve the paths BEFORE removing the record: afterwards the
			// daemon has forgotten where the files were.
			ts, err := s.Essaim.List()
			if err != nil {
				fail(w, http.StatusBadGateway, "essaim_unreachable", err.Error())
				return
			}
			found := false
			for _, t := range ts {
				if t.ID == id {
					dir, name, found = t.Dir, t.Name, true
					break
				}
			}
			if !found {
				fail(w, http.StatusNotFound, "not_found", "no such torrent")
				return
			}
		}

		if err := s.Essaim.Remove(id); err != nil {
			fail(w, http.StatusBadGateway, "essaim_error", err.Error())
			return
		}
		res := map[string]any{"ok": true, "removed": id}
		if withData {
			if err := removeData(dir, name); err != nil {
				// The entry is already gone, so this is a partial success and
				// saying so is more useful than a bare 500.
				res["data_deleted"] = false
				res["data_error"] = err.Error()
			} else {
				res["data_deleted"] = true
				res["path"] = fullPath(dir, name)
			}
		}
		writeJSON(w, http.StatusOK, res)
	}))

	// Pointers, not values: a field the browser omitted must stay omitted all
	// the way to the daemon, or changing the cap would silently reset the
	// ratio to zero.
	mux.HandleFunc("PATCH /api/torrents/{id}", s.authed(func(w http.ResponseWriter, r *http.Request, sess session) {
		var body struct {
			Seed      *bool `json:"seed"`
			UpLimit   *int  `json:"up_limit"`
			DownLimit *int  `json:"down_limit"`
			RatioPct  *int  `json:"ratio_limit_pct"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
			fail(w, http.StatusBadRequest, "validation_error", "body must be JSON")
			return
		}
		if body.Seed == nil && body.UpLimit == nil && body.DownLimit == nil && body.RatioPct == nil {
			fail(w, http.StatusBadRequest, "validation_error", "nothing to change")
			return
		}
		if body.UpLimit != nil && *body.UpLimit < 0 {
			fail(w, http.StatusBadRequest, "validation_error", "upload cap cannot be negative")
			return
		}
		if body.DownLimit != nil && *body.DownLimit < 0 {
			fail(w, http.StatusBadRequest, "validation_error", "download cap cannot be negative")
			return
		}
		if body.RatioPct != nil && *body.RatioPct < 0 {
			fail(w, http.StatusBadRequest, "validation_error", "ratio cannot be negative")
			return
		}
		t, err := s.Essaim.Patch(r.PathValue("id"), body.Seed, body.UpLimit, body.DownLimit, body.RatioPct)
		if err != nil {
			fail(w, http.StatusBadGateway, "essaim_error", err.Error())
			return
		}
		res := map[string]any{"ok": true, "torrent": t}
		// Unlike the others, a download cap takes effect when the torrent next
		// starts — the pacer takes its interval at job start, and restarting a
		// running job from essaim's control loop would hang its supervisor.
		// Saying so is better than a control that looks instant and is not.
		if body.DownLimit != nil {
			res["note"] = "download cap applies the next time this torrent starts"
		}
		writeJSON(w, http.StatusOK, res)
	}))

	mux.HandleFunc("POST /api/labels", s.authed(func(w http.ResponseWriter, r *http.Request, sess session) {
		var l bknclient.Label
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&l); err != nil {
			fail(w, http.StatusBadRequest, "validation_error", "body must be JSON")
			return
		}
		// user_id is never taken from the client: bkn stamps it from the token.
		l.UserID = ""
		var saved bknclient.Label
		err := s.withRefresh(w, sess, func(tok string) error {
			var e error
			saved, e = s.Bkn.PutLabel(tok, l)
			return e
		})
		if err != nil {
			fail(w, http.StatusBadGateway, "bkn_error", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "label": saved})
	}))

	mux.HandleFunc("GET /api/labels", s.authed(func(w http.ResponseWriter, r *http.Request, sess session) {
		var labels []bknclient.Label
		err := s.withRefresh(w, sess, func(tok string) error {
			var e error
			labels, e = s.Bkn.Labels(tok)
			return e
		})
		if err != nil {
			fail(w, http.StatusBadGateway, "bkn_error", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "count": len(labels), "labels": labels})
	}))

	mux.Handle("GET /", pageHandler())
	return logging(mux)
}

func logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		_ = strconv.Itoa(int(time.Since(start).Milliseconds()))
	})
}
