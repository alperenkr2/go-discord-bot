// Package config loads and validates the bot's runtime configuration from
// environment variables (optionally via a .env file).
package config

import (
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// Config holds every tunable the bot needs. Required fields are validated by
// Validate; everything else has a sensible default applied in Load.
type Config struct {
	// Discord
	BotToken   string // listener bot token ("Bot " prefix added by discordgo)
	UserToken  string // user-account token used to send OwO commands (raw Authorization header)
	ChannelURL string // Discord messages endpoint for the farm channel
	ChannelID  string // derived from ChannelURL; used for alert mentions
	AuthorID   string // owner user id, mentioned on alerts
	FriendID   string // optional battle partner user id

	// Telegram (optional). When both are empty, Telegram alerts are disabled.
	TelegramBotToken string
	TelegramChatID   string

	// Timing / behaviour
	DelayMin             time.Duration // min wait between hunt cycles
	DelayMax             time.Duration // max wait between hunt cycles
	FastDelay            time.Duration // fixed wait when fast mode is on
	BreakEvery           int           // take a long break every N cycles
	BreakDuration        time.Duration // length of the long break
	CaptchaReminderEvery time.Duration // re-alert interval while a captcha is pending
	CoverMessage         bool          // send the "Xs cooldown ..." cover message

	// Logging
	LogLevel slog.Level
	LogFile  string // optional; logs are tee'd here in addition to stderr
}

var channelIDRe = regexp.MustCompile(`channels/(\d+)/messages`)

// Load reads configuration from the environment. A .env file is loaded when
// present but is entirely optional — real environment variables work too.
func Load() *Config {
	_ = godotenv.Load() // .env is optional; ignore "not found"

	cfg := &Config{
		BotToken:   os.Getenv("BOT_TOKEN"),
		UserToken:  os.Getenv("BEARER_TOKEN"),
		ChannelURL: os.Getenv("CHANNEL_URL"),
		AuthorID:   os.Getenv("AUTHOR_ID"),
		FriendID:   os.Getenv("OTHER_AUTHOR_ID"),

		TelegramBotToken: os.Getenv("TELEGRAM_BOT_TOKEN"),
		TelegramChatID:   os.Getenv("TELEGRAM_CHAT_ID"),

		DelayMin:             envSeconds("DELAY_MIN_SECONDS", 30*time.Second),
		DelayMax:             envSeconds("DELAY_MAX_SECONDS", 120*time.Second),
		FastDelay:            envSeconds("FAST_DELAY_SECONDS", 13*time.Second),
		BreakEvery:           envInt("BREAK_EVERY", 10),
		BreakDuration:        envSeconds("BREAK_SECONDS", 240*time.Second),
		CaptchaReminderEvery: envSeconds("CAPTCHA_REMINDER_SECONDS", 60*time.Second),
		CoverMessage:         envBool("COVER_MESSAGE", true),

		LogLevel: parseLevel(os.Getenv("LOG_LEVEL")),
		LogFile:  os.Getenv("LOG_FILE"),
	}

	cfg.ChannelID = channelIDFromURL(cfg.ChannelURL)

	if cfg.BreakEvery < 1 {
		cfg.BreakEvery = 1
	}
	if cfg.DelayMax < cfg.DelayMin {
		cfg.DelayMax = cfg.DelayMin
	}

	return cfg
}

// Validate reports whether the required fields are present.
func (c *Config) Validate() error {
	var missing []string
	for _, f := range []struct {
		name, val string
	}{
		{"BOT_TOKEN", c.BotToken},
		{"BEARER_TOKEN", c.UserToken},
		{"CHANNEL_URL", c.ChannelURL},
		{"AUTHOR_ID", c.AuthorID},
	} {
		if strings.TrimSpace(f.val) == "" {
			missing = append(missing, f.name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("eksik zorunlu ortam değişkenleri: %s", strings.Join(missing, ", "))
	}
	return nil
}

// TelegramEnabled reports whether Telegram alerts are fully configured.
func (c *Config) TelegramEnabled() bool {
	return c.TelegramBotToken != "" && c.TelegramChatID != ""
}

func channelIDFromURL(u string) string {
	if m := channelIDRe.FindStringSubmatch(u); len(m) == 2 {
		return m[1]
	}
	return ""
}

func parseLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func envSeconds(key string, def time.Duration) time.Duration {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			return time.Duration(n) * time.Second
		}
	}
	return def
}

func envInt(key string, def int) int {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envBool(key string, def bool) bool {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return def
}
