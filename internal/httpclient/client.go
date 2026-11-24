package httpclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mosaseka/Discord_ResearchBriefBot/internal/security"
)

var (
	ErrTooLarge    = errors.New("HTTP response exceeds the configured limit")
	ErrInvalidMIME = errors.New("unexpected HTTP response MIME type")
	ErrStatus      = errors.New("unexpected HTTP response status")
)

type Validator interface {
	CheckURL(context.Context, *url.URL) error
}
type ValidatorFunc func(context.Context, *url.URL) error

func (f ValidatorFunc) CheckURL(ctx context.Context, u *url.URL) error { return f(ctx, u) }

type Config struct {
	UserAgent       string
	RequestTimeout  time.Duration
	DialTimeout     time.Duration
	MaxRedirects    int
	Attempts        int
	DefaultInterval time.Duration
}

func DefaultConfig() Config {
	return Config{UserAgent: "DiscordResearchBriefBot/1.0 (+https://github.com/mosaseka/Discord_ResearchBriefBot)",
		RequestTimeout: 25 * time.Second, DialTimeout: 5 * time.Second, MaxRedirects: 4, Attempts: 3, DefaultInterval: time.Second}
}

type Client struct {
	http      *http.Client
	validator Validator
	config    Config
	mu        sync.Mutex
	hosts     map[string]*hostGate
}

type hostGate struct {
	mu   sync.Mutex
	next time.Time
}

func New(config Config) (*Client, error) {
	config = defaults(config)
	policy := security.NewPolicy(nil, &net.Dialer{Timeout: config.DialTimeout, KeepAlive: 30 * time.Second})
	transport := &http.Transport{Proxy: nil, DialContext: policy.DialContext, ForceAttemptHTTP2: true,
		TLSHandshakeTimeout: config.DialTimeout, ResponseHeaderTimeout: config.RequestTimeout / 2,
		MaxIdleConns: 16, MaxIdleConnsPerHost: 2, IdleConnTimeout: 60 * time.Second}
	h := &http.Client{Transport: transport, Timeout: config.RequestTimeout}
	c := &Client{http: h, validator: policy, config: config, hosts: make(map[string]*hostGate)}
	h.CheckRedirect = c.checkRedirect
	return c, nil
}

// NewForTest accepts an isolated client and validator for httptest/fake use.
func NewForTest(h *http.Client, validator Validator, attempts int) *Client {
	if validator == nil {
		validator = ValidatorFunc(func(context.Context, *url.URL) error { return nil })
	}
	cfg := DefaultConfig()
	cfg.DefaultInterval = 0
	if attempts > 0 {
		cfg.Attempts = attempts
	}
	c := &Client{http: h, validator: validator, config: cfg, hosts: make(map[string]*hostGate)}
	h.CheckRedirect = c.checkRedirect
	return c
}

func defaults(c Config) Config {
	d := DefaultConfig()
	if c.UserAgent == "" {
		c.UserAgent = d.UserAgent
	}
	if c.RequestTimeout <= 0 {
		c.RequestTimeout = d.RequestTimeout
	}
	if c.DialTimeout <= 0 {
		c.DialTimeout = d.DialTimeout
	}
	if c.MaxRedirects <= 0 {
		c.MaxRedirects = d.MaxRedirects
	}
	if c.Attempts <= 0 {
		c.Attempts = d.Attempts
	}
	if c.DefaultInterval <= 0 {
		c.DefaultInterval = d.DefaultInterval
	}
	return c
}

func (c *Client) CloseIdleConnections() {
	if c != nil && c.http != nil {
		c.http.CloseIdleConnections()
	}
}

func (c *Client) Fetch(ctx context.Context, raw, accept string, allowedMIME []string, maxBytes int64) ([]byte, error) {
	if c == nil || c.http == nil || c.validator == nil {
		return nil, errors.New("HTTP client is not initialized")
	}
	if maxBytes <= 0 {
		return nil, errors.New("response limit must be positive")
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("parse request URL: %w", err)
	}
	if err := c.validator.CheckURL(ctx, u); err != nil {
		return nil, err
	}
	var last error
	for attempt := 0; attempt < c.config.Attempts; attempt++ {
		if err := c.wait(ctx, u.Hostname()); err != nil {
			return nil, err
		}
		body, retry, retryAfter, fetchErr := c.fetchOnce(ctx, u, accept, allowedMIME, maxBytes)
		if fetchErr == nil {
			return body, nil
		}
		last = fetchErr
		if !retry || attempt+1 == c.config.Attempts {
			break
		}
		if retryAfter <= 0 {
			retryAfter = time.Duration(1<<attempt) * 250 * time.Millisecond
		}
		if err := sleep(ctx, retryAfter); err != nil {
			return nil, err
		}
	}
	return nil, last
}

func (c *Client) fetchOnce(ctx context.Context, u *url.URL, accept string, allowed []string, max int64) ([]byte, bool, time.Duration, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, false, 0, err
	}
	req.Header.Set("User-Agent", c.config.UserAgent)
	req.Header.Set("Accept", accept)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, true, 0, fmt.Errorf("GET %s: %w", endpointLabel(u), err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 32<<10))
		return nil, true, retryAfter(resp), fmt.Errorf("%w: %s returned %d", ErrStatus, endpointLabel(u), resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, false, 0, fmt.Errorf("%w: %s returned %d", ErrStatus, endpointLabel(u), resp.StatusCode)
	}
	body, err := readResponse(resp, allowed, max)
	return body, false, 0, err
}

func readResponse(resp *http.Response, allowed []string, max int64) ([]byte, error) {
	if resp.ContentLength > max {
		return nil, ErrTooLarge
	}
	mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidMIME, err)
	}
	valid := false
	for _, candidate := range allowed {
		if strings.EqualFold(mediaType, candidate) {
			valid = true
			break
		}
	}
	if !valid {
		return nil, fmt.Errorf("%w: %q", ErrInvalidMIME, mediaType)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if int64(len(body)) > max {
		return nil, ErrTooLarge
	}
	return body, nil
}

func (c *Client) checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) > c.config.MaxRedirects {
		return errors.New("redirect limit exceeded")
	}
	if err := c.validator.CheckURL(req.Context(), req.URL); err != nil {
		return fmt.Errorf("validate redirect: %w", err)
	}
	return nil
}

func (c *Client) wait(ctx context.Context, host string) error {
	c.mu.Lock()
	gate := c.hosts[strings.ToLower(host)]
	if gate == nil {
		gate = &hostGate{}
		c.hosts[strings.ToLower(host)] = gate
	}
	c.mu.Unlock()
	gate.mu.Lock()
	defer gate.mu.Unlock()
	delay := c.interval(host)
	wait := time.Until(gate.next)
	if wait > 0 {
		if err := sleep(ctx, wait); err != nil {
			return err
		}
	}
	gate.next = time.Now().Add(delay)
	return nil
}

func (c *Client) interval(host string) time.Duration {
	switch strings.ToLower(host) {
	case "export.arxiv.org":
		return 3 * time.Second
	case "eutils.ncbi.nlm.nih.gov":
		return 350 * time.Millisecond
	case "api.crossref.org":
		return time.Second
	default:
		return c.config.DefaultInterval
	}
}

func retryAfter(resp *http.Response) time.Duration {
	raw := strings.TrimSpace(resp.Header.Get("Retry-After"))
	if seconds, err := strconv.ParseFloat(raw, 64); err == nil && seconds >= 0 {
		return time.Duration(seconds * float64(time.Second))
	}
	if when, err := http.ParseTime(raw); err == nil {
		return time.Until(when)
	}
	return 0
}

func endpointLabel(u *url.URL) string {
	if u == nil {
		return "<invalid>"
	}
	copy := *u
	copy.RawQuery = ""
	copy.ForceQuery = false
	copy.Fragment = ""
	return copy.Redacted()
}

func sleep(ctx context.Context, duration time.Duration) error {
	t := time.NewTimer(duration)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
