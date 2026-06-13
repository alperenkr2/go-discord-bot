package alert

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Telegram sends alerts to a chat via the Telegram Bot API. It uses the standard
// library only — no extra dependency.
type Telegram struct {
	token  string
	chatID string
	http   *http.Client
	logger *slog.Logger
}

func NewTelegram(token, chatID string, logger *slog.Logger) *Telegram {
	return &Telegram{
		token:  token,
		chatID: chatID,
		http:   &http.Client{Timeout: 10 * time.Second},
		logger: logger,
	}
}

func (t *Telegram) Notify(ctx context.Context, level Level, text string) {
	if err := t.Send(ctx, fmt.Sprintf("%s %s", levelEmoji(level), text)); err != nil {
		t.logger.Warn("telegram: send failed", "err", err)
	}
}

// Send posts text to the configured chat. On a non-OK response it returns an
// error that includes Telegram's own description (e.g. "Bad Request: chat not
// found"), which makes 400s diagnosable. It does not add a level emoji.
func (t *Telegram) Send(ctx context.Context, text string) error {
	endpoint := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", t.token)

	form := url.Values{}
	form.Set("chat_id", t.chatID)
	form.Set("text", text)
	form.Set("disable_web_page_preview", "true")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := t.http.Do(req)
	if err != nil {
		return fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func levelEmoji(level Level) string {
	switch level {
	case Critical:
		return "🚨"
	case Warn:
		return "⚠️"
	default:
		return "ℹ️"
	}
}
