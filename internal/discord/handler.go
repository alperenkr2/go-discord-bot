// Package discord wires the discordgo session: it registers message and
// connection handlers and routes the operator's control commands to the Farmer.
package discord

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"

	"go-discord-bot/internal/alert"
	"go-discord-bot/internal/config"
	"go-discord-bot/internal/farm"
	"go-discord-bot/internal/owo"
)

const (
	thumbsUp             = "👍"
	disconnectAlertAfter = 30 * time.Second
	oneShotTimeout       = 60 * time.Second
)

// Handler holds the dependencies the discord event handlers need.
type Handler struct {
	session *discordgo.Session
	cfg     *config.Config
	farmer  *farm.Farmer
	client  *owo.Client
	notify  alert.Notifier
	logger  *slog.Logger

	discMu    sync.Mutex
	discTimer *time.Timer
}

// New registers all handlers and intents on session and returns the Handler.
func New(session *discordgo.Session, cfg *config.Config, farmer *farm.Farmer, client *owo.Client, notify alert.Notifier, logger *slog.Logger) *Handler {
	h := &Handler{
		session: session,
		cfg:     cfg,
		farmer:  farmer,
		client:  client,
		notify:  notify,
		logger:  logger,
	}

	session.Identify.Intents = discordgo.IntentsGuilds |
		discordgo.IntentsGuildMessages |
		discordgo.IntentsMessageContent |
		discordgo.IntentsDirectMessages

	session.AddHandler(h.onMessageCreate)
	session.AddHandler(h.onConnect)
	session.AddHandler(h.onDisconnect)
	session.AddHandler(h.onResumed)

	return h
}

func (h *Handler) onMessageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	if s.State == nil || s.State.User == nil || m.Author == nil {
		return
	}
	if m.Author.ID == s.State.User.ID {
		return // ignore our own messages
	}

	content := strings.TrimSpace(m.Content)

	// React to OwO's messages (captcha/ban/hunt/inventory).
	h.farmer.HandleOwO(content)

	// Handle operator control commands.
	switch strings.ToLower(content) {
	case "sa":
		h.farmer.ClearCaptcha()
		h.reply(m.ChannelID, "as ben bot")
	case "owoh":
		h.reply(m.ChannelID, "başlıyorum")
		h.farmer.Start(m.ChannelID)
	case "owob fr":
		h.farmer.SetBattleFriends(true)
		h.reply(m.ChannelID, "battle with friends aktif edildi")
		h.farmer.Start(m.ChannelID)
	case "owo fast":
		h.farmer.SetFast(true)
		h.reply(m.ChannelID, "fast mode açıldı")
		h.farmer.Start(m.ChannelID)
	case "dur":
		h.farmer.Stop()
		h.react(m, thumbsUp)
	case "sell ww":
		go h.sellWeapons(m.ChannelID)
	case "ping":
		go h.ping(m.ChannelID, m.ID)
	}
}

func (h *Handler) ping(channelID, messageID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := h.client.Send(ctx, "ping"); err != nil {
		h.logger.Warn("ping failed", "err", err)
		h.reply(channelID, "mesaj gönderilemedi. token kontrol ediniz.")
		h.notify.Notify(context.Background(), alert.Critical, "ping başarısız: kullanıcı token (BEARER_TOKEN) geçersiz olabilir.")
		return
	}
	h.reactID(channelID, messageID, thumbsUp)
}

func (h *Handler) sellWeapons(channelID string) {
	ctx, cancel := context.WithTimeout(context.Background(), oneShotTimeout)
	defer cancel()

	if err := h.client.SellWeapons(ctx); err != nil {
		h.logger.Warn("sell weapons failed", "err", err)
		h.reply(channelID, "weapon satışı başarısız oldu")
		return
	}
	h.reply(channelID, "weaponlar satıldı")
}

func (h *Handler) onConnect(_ *discordgo.Session, _ *discordgo.Connect) {
	h.logger.Info("discord connected")
	h.cancelDisconnectTimer()
}

func (h *Handler) onResumed(_ *discordgo.Session, _ *discordgo.Resumed) {
	h.logger.Info("discord session resumed")
	h.cancelDisconnectTimer()
}

// onDisconnect fires on every reconnect cycle, so we only alert if the outage
// lasts longer than disconnectAlertAfter (discordgo reconnects automatically).
func (h *Handler) onDisconnect(_ *discordgo.Session, _ *discordgo.Disconnect) {
	h.logger.Warn("discord disconnected (auto-reconnect in progress)")

	h.discMu.Lock()
	defer h.discMu.Unlock()
	if h.discTimer != nil {
		h.discTimer.Stop()
	}
	h.discTimer = time.AfterFunc(disconnectAlertAfter, func() {
		h.notify.Notify(context.Background(), alert.Warn, "Discord bağlantısı 30+ saniyedir kopuk — yeniden bağlanılamıyor olabilir.")
	})
}

func (h *Handler) cancelDisconnectTimer() {
	h.discMu.Lock()
	defer h.discMu.Unlock()
	if h.discTimer != nil {
		h.discTimer.Stop()
		h.discTimer = nil
	}
}

func (h *Handler) reply(channelID, text string) {
	if _, err := h.session.ChannelMessageSend(channelID, text); err != nil {
		h.logger.Warn("channel message send failed", "err", err, "channel", channelID)
	}
}

func (h *Handler) react(m *discordgo.MessageCreate, emoji string) {
	h.reactID(m.ChannelID, m.ID, emoji)
}

func (h *Handler) reactID(channelID, messageID, emoji string) {
	if err := h.session.MessageReactionAdd(channelID, messageID, emoji); err != nil {
		h.logger.Warn("reaction add failed", "err", err)
	}
}
