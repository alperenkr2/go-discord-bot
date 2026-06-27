// Command bot runs the OwO farming automation: it connects to Discord, farms on
// command, and alerts the operator (Telegram + channel mention) on captchas and
// errors.
package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/bwmarrin/discordgo"

	"go-discord-bot/internal/alert"
	"go-discord-bot/internal/config"
	"go-discord-bot/internal/discord"
	"go-discord-bot/internal/farm"
	"go-discord-bot/internal/owo"
)

func main() {
	cfg := config.Load()
	logger := newLogger(cfg)

	if err := cfg.Validate(); err != nil {
		logger.Error("yapılandırma hatası", "err", err)
		os.Exit(1)
	}

	session, err := discordgo.New("Bot " + cfg.BotToken)
	if err != nil {
		logger.Error("discord session oluşturulamadı", "err", err)
		os.Exit(1)
	}

	notifier := buildNotifier(cfg, session, logger)
	client := owo.NewClient(cfg.UserToken, cfg.ChannelURL, logger)
	farmer := farm.New(cfg, client, notifier, session, logger)
	discord.New(session, cfg, farmer, client, notifier, logger)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := session.Open(); err != nil {
		logger.Error("discord bağlantısı açılamadı", "err", err)
		os.Exit(1)
	}
	defer session.Close()

	logger.Info("bot çevrimiçi")
	notifier.Notify(context.Background(), alert.Info, "OwO bot açıldı ve çalışıyor.")

	if cfg.AutoStart && cfg.ChannelID != "" {
		logger.Info("auto-start: farming başlatılıyor", "channel", cfg.ChannelID)
		farmer.Start(cfg.ChannelID)
	}

	<-ctx.Done()

	logger.Info("kapatılıyor")
	farmer.Stop()
	notifier.Notify(context.Background(), alert.Info, "OwO bot kapatıldı.")
}

func newLogger(cfg *config.Config) *slog.Logger {
	w := io.Writer(os.Stderr)
	if cfg.LogFile != "" {
		f, err := os.OpenFile(cfg.LogFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			fmt.Fprintf(os.Stderr, "log dosyası açılamadı (%s): %v — stderr kullanılıyor\n", cfg.LogFile, err)
		} else {
			w = io.MultiWriter(os.Stderr, f)
		}
	}
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: cfg.LogLevel}))
}

func buildNotifier(cfg *config.Config, session *discordgo.Session, logger *slog.Logger) alert.Notifier {
	notifiers := []alert.Notifier{alert.NewLog(logger)}

	if cfg.ChannelID != "" {
		notifiers = append(notifiers, alert.NewDiscordChannel(session, cfg.ChannelID, cfg.AuthorID, logger))
	} else {
		logger.Warn("CHANNEL_URL'den kanal id çıkarılamadı; kanal mention uyarıları devre dışı")
	}

	if cfg.TelegramEnabled() {
		notifiers = append(notifiers, alert.NewTelegram(cfg.TelegramBotToken, cfg.TelegramChatID, logger))
	} else {
		logger.Warn("Telegram yapılandırılmadı; uyarılar yalnızca kanal mention + log ile gidecek")
	}

	return alert.NewMulti(notifiers...)
}
