package wireguard

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/archit3ckt/nullwatch/internal/config"
)

// Client talks to wg-easy's REST API to create peers programmatically, so
// adding a client never requires opening its admin UI to the public
// internet — nullwatch runs on the VPS itself, so these requests go over
// localhost and never touch the DOCKER-USER lockdown at all.
type Client struct {
	baseURL  string
	password string
	http     *http.Client
	cookie   string
}

// NewClient builds a Client targeting wg-easy's web UI on the local host,
// where its port is published.
func NewClient(cfg *config.WireGuardConfig) *Client {
	return &Client{
		baseURL:  fmt.Sprintf("http://127.0.0.1:%d", cfg.WebUIPort),
		password: cfg.WebUIPassword,
		http: &http.Client{
			Timeout: 10 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

func unexpectedStatus(action string, resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return fmt.Errorf("%s: unexpected status %d: %s", action, resp.StatusCode, body)
}

func (c *Client) login() error {
	body, err := json.Marshal(map[string]string{"password": c.password})
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/api/session", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return unexpectedStatus("wg-easy login", resp)
	}

	var cookies []string
	for _, ck := range resp.Cookies() {
		cookies = append(cookies, ck.String())
	}
	c.cookie = strings.Join(cookies, "; ")
	if c.cookie == "" {
		return fmt.Errorf("wg-easy login: no session cookie returned")
	}
	return nil
}

func (c *Client) authedRequest(method, path string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequest(method, c.baseURL+path, body)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Cookie", c.cookie)
	return c.http.Do(req)
}

type createdClient struct {
	ID string `json:"id"`
}

// CreatePeer logs in, creates a new WireGuard peer with the given name, and
// returns its id and the plaintext .conf file content.
func (c *Client) CreatePeer(name string) (id string, conf string, err error) {
	if err := c.login(); err != nil {
		return "", "", fmt.Errorf("wg-easy: %w", err)
	}

	body, err := json.Marshal(map[string]string{"name": name})
	if err != nil {
		return "", "", err
	}
	resp, err := c.authedRequest(http.MethodPost, "/api/wireguard/client", bytes.NewReader(body))
	if err != nil {
		return "", "", fmt.Errorf("wg-easy: create peer: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", unexpectedStatus("create peer", resp)
	}

	var created createdClient
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		return "", "", fmt.Errorf("wg-easy: decode created peer: %w", err)
	}
	if created.ID == "" {
		return "", "", fmt.Errorf("wg-easy: create peer: response had no id")
	}

	confResp, err := c.authedRequest(http.MethodGet, "/api/wireguard/client/"+created.ID+"/configuration", nil)
	if err != nil {
		return "", "", fmt.Errorf("wg-easy: fetch config: %w", err)
	}
	defer confResp.Body.Close()
	if confResp.StatusCode != http.StatusOK {
		return "", "", unexpectedStatus("fetch peer config", confResp)
	}

	confBytes, err := io.ReadAll(confResp.Body)
	if err != nil {
		return "", "", fmt.Errorf("wg-easy: read config: %w", err)
	}
	return created.ID, string(confBytes), nil
}
