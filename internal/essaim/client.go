// Package essaim talks to a running essaim daemon.
//
// The daemon is single-actor: one loop owns the job table and answers each
// request inline. So this client stays deliberately dumb — no pooling, no
// retries, no concurrency of its own. A control-plane call takes microseconds
// against a download that takes minutes, and anything cleverer here would only
// invent failure modes the daemon does not have.
package essaim

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Client struct {
	BaseURL string
	HTTP    *http.Client
}

func New(baseURL string) *Client {
	return &Client{BaseURL: baseURL, HTTP: &http.Client{Timeout: 15 * time.Second}}
}

// Torrent mirrors the daemon's record. Progress is re-derived from the files
// on disk by essaim itself, so nothing here is a cached truth we have to age.
type Torrent struct {
	ID          string `json:"id"`
	Source      string `json:"source"`
	Dir         string `json:"dir"`
	Infohash    string `json:"infohash"`
	Name        string `json:"name"`
	State       string `json:"state"`
	PiecesDone  int    `json:"pieces_done"`
	PiecesTotal int    `json:"pieces_total"`
	Bytes       int64  `json:"bytes"`
	Peers       int    `json:"peers"`
	Uploaded    int64  `json:"uploaded_bytes"`
	// The daemon READS seed as an int (parse_int on the request body) and
	// WRITES it as a bool. Add() therefore sends 1 while this reads true —
	// an asymmetry in essaim, not a mistake here.
	Seed         bool `json:"seed"`
	UpLimitKBs   int  `json:"up_limit_kbs"`
	DownLimitKBs int  `json:"down_limit_kbs"`
	// RatioPct is a stop condition in percent of what was downloaded: 50 is
	// 0.5x, 200 is 2x, 0 never stops. Percent rather than a float because
	// essaim has no floats, and the unit is in the name so nobody has to guess.
	RatioPct int    `json:"ratio_limit_pct"`
	Error    string `json:"error"`
}

type Health struct {
	OK      bool   `json:"ok"`
	Version string `json:"version"`
	Jobs    int    `json:"jobs"`
}

func (c *Client) do(method, path string, body any, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.BaseURL+path, rdr)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("essaim daemon unreachable at %s: %w", c.BaseURL, err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return err
	}
	if res.StatusCode >= 400 {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(raw, &e)
		if e.Error == "" {
			e.Error = res.Status
		}
		return errors.New(e.Error)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(raw, out)
}

func (c *Client) Health() (Health, error) {
	var h Health
	err := c.do(http.MethodGet, "/_health", nil, &h)
	return h, err
}

func (c *Client) List() ([]Torrent, error) {
	var r struct {
		Torrents []Torrent `json:"torrents"`
	}
	if err := c.do(http.MethodGet, "/torrents", nil, &r); err != nil {
		return nil, err
	}
	return r.Torrents, nil
}

// Add takes a magnet, a .torrent path or an https URL — essaim resolves which.
func (c *Client) Add(source, dir string, seed bool, upLimit, downLimit, ratioPct int) (Torrent, error) {
	body := map[string]any{"source": source, "dir": dir}
	if seed {
		body["seed"] = 1
	}
	if upLimit > 0 {
		body["up_limit"] = upLimit
	}
	if downLimit > 0 {
		body["down_limit"] = downLimit
	}
	if ratioPct > 0 {
		body["ratio_limit_pct"] = ratioPct
	}
	var r struct {
		Torrent Torrent `json:"torrent"`
	}
	err := c.do(http.MethodPost, "/torrents", body, &r)
	return r.Torrent, err
}

func (c *Client) Remove(id string) error {
	return c.do(http.MethodDelete, "/torrents/"+id, nil, nil)
}

// Patch adjusts a torrent that already exists. A nil field is left alone by the
// daemon, so a caller changing one setting cannot silently reset another —
// which is why these are pointers rather than zero-valued ints.
func (c *Client) Patch(id string, seed *bool, upLimit, downLimit, ratioPct *int) (Torrent, error) {
	body := map[string]any{}
	if seed != nil {
		// The daemon parses seed as an int on input and reports it as a bool.
		// It accepts either now, but sending the int keeps this working
		// against an older daemon too.
		if *seed {
			body["seed"] = 1
		} else {
			body["seed"] = 0
		}
	}
	if upLimit != nil {
		body["up_limit"] = *upLimit
	}
	if downLimit != nil {
		body["down_limit"] = *downLimit
	}
	if ratioPct != nil {
		body["ratio_limit_pct"] = *ratioPct
	}
	var r struct {
		Torrent Torrent `json:"torrent"`
	}
	err := c.do(http.MethodPatch, "/torrents/"+id, body, &r)
	return r.Torrent, err
}
