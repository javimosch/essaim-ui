// Package bknclient is the bkn side of essaim-ui.
//
// The important property: every per-user call carries the USER's access token,
// never an admin token. bkn's collection access policies then decide what that
// user can see, so ownership is enforced by the store rather than by this
// process remembering to filter. essaim-ui cannot leak one user's labels to
// another even if a handler forgets a check, because the query it sends is
// already scoped by the token it sent.
//
// The admin token appears in exactly one place — Setup, which declares the
// collections — and never on the serving path.
package bknclient

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

type Client struct {
	BaseURL string
	HTTP    *http.Client
}

func New(baseURL string) *Client {
	return &Client{BaseURL: baseURL, HTTP: &http.Client{Timeout: 15 * time.Second}}
}

type Tokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	Org          string `json:"org"`
	User         struct {
		ID    string `json:"id"`
		Email string `json:"email"`
	} `json:"user"`
}

type apiError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

func (c *Client) do(method, path, token string, body any, out any) error {
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
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("bkn unreachable at %s: %w", c.BaseURL, err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if res.StatusCode >= 400 {
		var e struct {
			Error apiError `json:"error"`
		}
		_ = json.Unmarshal(raw, &e)
		msg := e.Error.Message
		if msg == "" {
			msg = res.Status
		}
		// 401 is worth distinguishing: it is the one a client can act on by
		// signing in again, which is exactly what the web layer does with it.
		if res.StatusCode == http.StatusUnauthorized {
			return fmt.Errorf("%w: %s", ErrUnauthenticated, msg)
		}
		return errors.New(msg)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(raw, out)
}

var ErrUnauthenticated = errors.New("not authenticated")

func (c *Client) Health() error { return c.do(http.MethodGet, "/_health", "", nil, nil) }

func (c *Client) Login(email, password, org string) (Tokens, error) {
	body := map[string]any{"email": email, "password": password}
	if org != "" {
		body["org"] = org
	}
	var r struct {
		Tokens Tokens `json:"tokens"`
	}
	err := c.do(http.MethodPost, "/v1/auth/login", "", body, &r)
	return r.Tokens, err
}

func (c *Client) Refresh(refresh string) (Tokens, error) {
	var r struct {
		Tokens Tokens `json:"tokens"`
	}
	err := c.do(http.MethodPost, "/v1/auth/refresh", "", map[string]any{"refresh_token": refresh}, &r)
	return r.Tokens, err
}

// Label is a user's own note on a torrent. The user_id field is NOT set here:
// a scoped create stamps it from the token, so a client cannot write into
// somebody else's tenancy even by trying.
type Label struct {
	ID       string `json:"id,omitempty"`
	Infohash string `json:"infohash"`
	Source   string `json:"source,omitempty"`
	Name     string `json:"name,omitempty"`
	Note     string `json:"note,omitempty"`
	Tag      string `json:"tag,omitempty"`
	UserID   string `json:"user_id,omitempty"`
}

const LabelsRef = "essaimui/labels"

func (c *Client) Labels(token string) ([]Label, error) {
	var r struct {
		Records []Label `json:"records"`
	}
	if err := c.do(http.MethodGet, "/v1/store/"+LabelsRef+"?limit=500", token, nil, &r); err != nil {
		return nil, err
	}
	return r.Records, nil
}

func (c *Client) PutLabel(token string, l Label) (Label, error) {
	var r struct {
		Record Label `json:"record"`
	}
	path := "/v1/store/" + LabelsRef
	if l.ID != "" {
		path += "?id=" + url.QueryEscape(l.ID)
	}
	err := c.do(http.MethodPost, path, token, l, &r)
	return r.Record, err
}

func (c *Client) DeleteLabel(token, id string) error {
	return c.do(http.MethodDelete, "/v1/store/"+LabelsRef+"/"+url.PathEscape(id), token, nil, nil)
}

// Setup declares the collection and its access policy. This is the only call
// that uses the admin token, and it is a separate command rather than
// something the server does at boot — a process that can rewrite its own
// authorization rules at startup is a process whose rules mean less.
func (c *Client) Setup(adminToken string) error {
	body := map[string]any{
		"access": map[string]any{
			"owner_field": "user_id",
			"rules": map[string]any{
				"read": "owner", "create": "owner", "update": "owner", "delete": "owner",
			},
		},
	}
	return c.do(http.MethodPut, "/v1/store/"+LabelsRef, adminToken, body, nil)
}
