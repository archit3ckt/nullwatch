package adguard

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/archit3ckt/nullwatch/internal/config"
)

// Client talks to AdGuard Home's REST API to manage DNS rewrites
// programmatically, so other modules (Traefik) can register hostnames
// without a human touching the AdGuard UI.
type Client struct {
	baseURL  string
	user     string
	password string
	http     *http.Client
}

// NewClient builds a Client targeting AdGuard's web UI on the local host,
// where its HTTP port is published.
func NewClient(cfg *config.AdGuardConfig) *Client {
	return &Client{
		baseURL:  fmt.Sprintf("http://127.0.0.1:%d", cfg.HTTPPort),
		user:     cfg.AdminUser,
		password: cfg.AdminPassword,
		http:     &http.Client{Timeout: 5 * time.Second},
	}
}

type rewriteEntry struct {
	Domain string `json:"domain"`
	Answer string `json:"answer"`
}

// WaitReady polls AdGuard's status endpoint until it responds or timeout
// elapses. AdGuard needs a moment after `up -d` before its API is reachable.
func (c *Client) WaitReady(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		req, err := http.NewRequest(http.MethodGet, c.baseURL+"/control/status", nil)
		if err != nil {
			return err
		}
		req.SetBasicAuth(c.user, c.password)

		resp, err := c.http.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
			lastErr = fmt.Errorf("status endpoint returned %d", resp.StatusCode)
		} else {
			lastErr = err
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("adguard not ready after %s: %w", timeout, lastErr)
}

// ListRewrites returns all currently configured DNS rewrites.
func (c *Client) ListRewrites() ([]rewriteEntry, error) {
	req, err := http.NewRequest(http.MethodGet, c.baseURL+"/control/rewrite/list", nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(c.user, c.password)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list rewrites: unexpected status %d", resp.StatusCode)
	}

	var entries []rewriteEntry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return nil, fmt.Errorf("decode rewrite list: %w", err)
	}
	return entries, nil
}

func (c *Client) addRewrite(domain, answer string) error {
	body, err := json.Marshal(rewriteEntry{Domain: domain, Answer: answer})
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/control/rewrite/add", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(c.user, c.password)

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("add rewrite %s -> %s: unexpected status %d", domain, answer, resp.StatusCode)
	}
	return nil
}

// EnsureRewrite adds a DNS rewrite (domain -> answer) if it doesn't already
// exist, making registration idempotent across repeated runs.
func (c *Client) EnsureRewrite(domain, answer string) error {
	entries, err := c.ListRewrites()
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.Domain == domain && e.Answer == answer {
			return nil // already present
		}
	}
	return c.addRewrite(domain, answer)
}
