package owo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// ErrUnauthorized is returned when Discord rejects the user token (401/403).
// The caller should stop and alert rather than retry.
var ErrUnauthorized = errors.New("owo: unauthorized (check BEARER_TOKEN)")

const (
	maxSendAttempts = 3
	minSendGap      = time.Second // floor between two sends (user rate-limit guard)
	defaultBackoff  = 2 * time.Second
)

// Client sends OwO commands to a channel as the user, via Discord's REST API.
type Client struct {
	http       *http.Client
	userToken  string
	channelURL string
	logger     *slog.Logger

	mu       sync.Mutex
	nextSend time.Time
}

func NewClient(userToken, channelURL string, logger *slog.Logger) *Client {
	return &Client{
		http:       &http.Client{Timeout: 15 * time.Second},
		userToken:  userToken,
		channelURL: channelURL,
		logger:     logger,
	}
}

// Send posts a single message, retrying on transient failures and honouring
// Discord's rate-limit (429) Retry-After. It returns ErrUnauthorized for token
// problems (no retry) and a wrapped error after exhausting attempts.
func (c *Client) Send(ctx context.Context, content string) error {
	c.throttle()

	var lastErr error
	for attempt := 1; attempt <= maxSendAttempts; attempt++ {
		retryAfter, err := c.post(ctx, content)
		if err == nil {
			return nil
		}
		lastErr = err

		if errors.Is(err, ErrUnauthorized) {
			return err
		}

		backoff := time.Duration(attempt) * time.Second
		if retryAfter > 0 {
			backoff = retryAfter
			c.logger.Warn("owo rate limited, backing off", "retry_after", retryAfter, "attempt", attempt)
		}
		if attempt < maxSendAttempts && !sleepCtx(ctx, backoff) {
			return ctx.Err()
		}
	}
	return fmt.Errorf("owo: send %q failed after %d attempts: %w", content, maxSendAttempts, lastErr)
}

// post performs one HTTP attempt. It returns a Retry-After hint (for 429s).
func (c *Client) post(ctx context.Context, content string) (time.Duration, error) {
	body, err := json.Marshal(map[string]any{
		"content": content,
		"nonce":   newNonce(),
		"tts":     false,
	})
	if err != nil {
		return 0, fmt.Errorf("owo: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.channelURL, bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("owo: new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", c.userToken)

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("owo: do request: %w", err)
	}
	defer resp.Body.Close()
	defer io.Copy(io.Discard, resp.Body) // drain so the connection can be reused

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return 0, nil
	case resp.StatusCode == http.StatusUnauthorized, resp.StatusCode == http.StatusForbidden:
		return 0, ErrUnauthorized
	case resp.StatusCode == http.StatusTooManyRequests:
		return retryAfter(resp), fmt.Errorf("owo: rate limited (429)")
	default:
		return 0, fmt.Errorf("owo: unexpected status %d", resp.StatusCode)
	}
}

// throttle enforces a minimum gap between consecutive sends.
func (c *Client) throttle() {
	c.mu.Lock()
	now := time.Now()
	wait := time.Duration(0)
	if c.nextSend.After(now) {
		wait = c.nextSend.Sub(now)
	}
	c.nextSend = now.Add(wait + minSendGap)
	c.mu.Unlock()

	if wait > 0 {
		time.Sleep(wait)
	}
}

// --- typed command helpers -------------------------------------------------

func (c *Client) Hunt(ctx context.Context) error      { return c.Send(ctx, "owo h") }
func (c *Client) Pray(ctx context.Context) error      { return c.Send(ctx, "owo pray") }
func (c *Client) Inventory(ctx context.Context) error { return c.Send(ctx, "owo inv") }

// Battle attacks; if friendID is set it battles that user.
func (c *Client) Battle(ctx context.Context, friendID string) error {
	cmd := "owo b"
	if friendID != "" {
		cmd = "owo b <@" + friendID + ">"
	}
	return c.Send(ctx, cmd)
}

// SellWeapons checks weapons then sells each rarity tier, spacing the commands
// out as OwO expects. Honours ctx cancellation between steps.
func (c *Client) SellWeapons(ctx context.Context) error {
	steps := []struct {
		cmd  string
		wait time.Duration
	}{
		{"owo wc all", 5 * time.Second},
		{"owo sell uncommonweapons", 3 * time.Second},
		{"owo sell commonweapons", 3 * time.Second},
		{"owo sell rareweapons", 3 * time.Second},
		{"owo sell epicweapons", 0},
	}
	for _, s := range steps {
		if err := c.Send(ctx, s.cmd); err != nil {
			return err
		}
		if s.wait > 0 && !sleepCtx(ctx, s.wait) {
			return ctx.Err()
		}
	}
	return nil
}

func newNonce() string {
	return strconv.FormatUint(rand.Uint64(), 10)
}

func retryAfter(resp *http.Response) time.Duration {
	if v := resp.Header.Get("Retry-After"); v != "" {
		if secs, err := strconv.ParseFloat(v, 64); err == nil && secs > 0 {
			return time.Duration(secs * float64(time.Second))
		}
	}
	return defaultBackoff
}

// sleepCtx sleeps for d, returning false if ctx is cancelled first.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
