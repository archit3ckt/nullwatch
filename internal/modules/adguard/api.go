package adguard

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/archit3ckt/nullwatch/internal/config"
)

// unexpectedStatus builds an error that includes AdGuard's own response
// body — its API returns a plain-text reason on failure (validation errors,
// etc.), and swallowing that turned every failure into an unhelpful bare
// status code.
func unexpectedStatus(action string, resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return fmt.Errorf("%s: unexpected status %d: %s", action, resp.StatusCode, body)
}

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
// where its HTTP port is published. Redirects are never followed: before
// first-run setup completes, AdGuard 302s every /control/* route to
// /install.html, and silently following that could turn a POST into a GET
// and make a failed call look like it succeeded. Every method here checks
// status codes explicitly instead.
func NewClient(cfg *config.AdGuardConfig) *Client {
	return &Client{
		baseURL:  fmt.Sprintf("http://127.0.0.1:%d", cfg.HTTPPort),
		user:     cfg.AdminUser,
		password: cfg.AdminPassword,
		http: &http.Client{
			Timeout: 5 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

type rewriteEntry struct {
	Domain string `json:"domain"`
	Answer string `json:"answer"`
}

type installRequest struct {
	Web struct {
		IP   string `json:"ip"`
		Port int    `json:"port"`
	} `json:"web"`
	DNS struct {
		IP   string `json:"ip"`
		Port int    `json:"port"`
	} `json:"dns"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// CompleteInstall finishes AdGuard's first-run setup via its own install
// API, so it never falls back to requiring the interactive setup wizard in
// the browser. Most of AdGuard's /control/* API 404s until this completes.
// If the instance is already configured (e.g. a later re-run), this
// endpoint responds 403 (confirmed against a real running instance —
// initially assumed 404, which was wrong), treated as success either way.
func (c *Client) CompleteInstall(httpPort, dnsPort int) error {
	req := installRequest{Username: c.user, Password: c.password}
	req.Web.IP = "0.0.0.0"
	req.Web.Port = httpPort
	req.DNS.IP = "0.0.0.0"
	req.DNS.Port = dnsPort

	body, err := json.Marshal(req)
	if err != nil {
		return err
	}

	httpReq, err := http.NewRequest(http.MethodPost, c.baseURL+"/control/install/configure", bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK, http.StatusNotFound, http.StatusForbidden:
		return nil
	default:
		return unexpectedStatus("complete install", resp)
	}
}

// defaultUpstreamDNS are plain IP addresses, deliberately not a DoH/DoT
// hostname. AdGuard's own factory default upstream is a DoH endpoint that
// needs its own bootstrap DNS servers to resolve first — on at least one
// real deployment those bootstrap servers were unreachable (timing out over
// plain UDP, and unreachable entirely over IPv6 since this stack's Docker
// network isn't configured for IPv6), which broke AdGuard's own outbound
// DNS resolution entirely, taking blocklist fetching and rewrite lookups
// down with it. A plain IP upstream has no bootstrap step to fail.
var defaultUpstreamDNS = []string{"9.9.9.9", "1.1.1.1"}

// SetUpstreamDNS overrides AdGuard's upstream DNS servers. Called as part
// of Bootstrap so a fresh instance never relies on AdGuard's own DoH-based
// factory default, which needs bootstrap DNS resolution that isn't
// guaranteed to work on every network.
func (c *Client) SetUpstreamDNS(servers []string) error {
	body, err := json.Marshal(map[string]any{
		"upstream_dns":  servers,
		"bootstrap_dns": servers,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/control/dns_config", bytes.NewReader(body))
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
		return unexpectedStatus("set upstream dns", resp)
	}
	return nil
}

type filteringStatus struct {
	Filters []struct {
		URL string `json:"url"`
	} `json:"filters"`
}

func (c *Client) existingFilterURLs() (map[string]bool, error) {
	req, err := http.NewRequest(http.MethodGet, c.baseURL+"/control/filtering/status", nil)
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
		return nil, unexpectedStatus("filtering status", resp)
	}

	var status filteringStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, fmt.Errorf("decode filtering status: %w", err)
	}

	urls := map[string]bool{}
	for _, f := range status.Filters {
		urls[f.URL] = true
	}
	return urls, nil
}

// addURLTimeout is longer than the client's default: AdGuard downloads and
// validates a blocklist synchronously before responding to /add_url, and
// large lists (OISD Big and friends) can take well over 5 seconds on a
// fresh fetch.
const addURLTimeout = 90 * time.Second

// EnsureFilters registers each blocklist URL that isn't already present,
// making it idempotent across repeated runs. Keeps going if one URL fails
// (slow/unreachable list) rather than aborting the rest — collects every
// error and reports them together at the end.
func (c *Client) EnsureFilters(blocklists []string) error {
	existing, err := c.existingFilterURLs()
	if err != nil {
		return err
	}

	var errs []string
	for i, url := range blocklists {
		if existing[url] {
			continue
		}

		body, err := json.Marshal(map[string]any{
			"name":      fmt.Sprintf("blocklist-%d", i+1),
			"url":       url,
			"whitelist": false,
		})
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), addURLTimeout)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/control/filtering/add_url", bytes.NewReader(body))
		if err != nil {
			cancel()
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		req.SetBasicAuth(c.user, c.password)

		resp, err := c.http.Do(req)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", url, err))
			cancel()
			continue
		}
		if resp.StatusCode != http.StatusOK {
			errs = append(errs, unexpectedStatus(fmt.Sprintf("add filter %s", url), resp).Error())
			resp.Body.Close()
			cancel()
			continue
		}
		resp.Body.Close()
		cancel()
	}
	if len(errs) > 0 {
		return fmt.Errorf("%d blocklist(s) failed to register:\n%s", len(errs), strings.Join(errs, "\n"))
	}
	return nil
}

// Bootstrap brings a freshly-started AdGuard container to a configured,
// idempotent state: wait for the HTTP server, then complete first-run setup
// if needed. Deliberately doesn't register blocklists — that's a separate,
// much slower step (EnsureFilters) callers should run last, after anything
// that depends on AdGuard's API responding quickly (like the DNS rewrite
// AdGuard+Traefik wiring needs) — a slow or stuck blocklist fetch can tie up
// AdGuard's API entirely, and unrelated calls queued behind it will time out
// too even though they have nothing to do with blocklists.
func (c *Client) Bootstrap(httpPort, dnsPort int) error {
	if err := c.WaitReady(20 * time.Second); err != nil {
		return fmt.Errorf("wait for adguard: %w", err)
	}
	if err := c.CompleteInstall(httpPort, dnsPort); err != nil {
		return fmt.Errorf("complete install: %w", err)
	}
	if err := c.SetUpstreamDNS(defaultUpstreamDNS); err != nil {
		return fmt.Errorf("set upstream dns: %w", err)
	}
	return nil
}

// WaitReady polls AdGuard's status endpoint until it responds or timeout
// elapses. AdGuard needs a moment after `up -d` before its API is reachable.
// WaitReady only confirms the HTTP server is up and listening — not that
// it's configured. Before first-run setup completes, AdGuard 302s
// /control/status to /install.html rather than answering it directly, so
// any response at all (redirect included, since c.http doesn't follow
// them) counts as proof of life; CompleteInstall handles the rest.
func (c *Client) WaitReady(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := c.http.Get(c.baseURL + "/control/status")
		if err == nil {
			resp.Body.Close()
			return nil
		}
		lastErr = err
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
		return nil, unexpectedStatus("list rewrites", resp)
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
		return unexpectedStatus(fmt.Sprintf("add rewrite %s -> %s", domain, answer), resp)
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
