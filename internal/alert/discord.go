package alert

import (
	"context"
	"log/slog"

	"github.com/bwmarrin/discordgo"
)

// DiscordChannel posts Warn/Critical alerts into the farm channel and mentions
// the owner. Info-level alerts are skipped to keep the channel quiet — they
// still reach the logs and Telegram.
type DiscordChannel struct {
	session   *discordgo.Session
	channelID string
	authorID  string
	logger    *slog.Logger
}

func NewDiscordChannel(session *discordgo.Session, channelID, authorID string, logger *slog.Logger) *DiscordChannel {
	return &DiscordChannel{
		session:   session,
		channelID: channelID,
		authorID:  authorID,
		logger:    logger,
	}
}

func (d *DiscordChannel) Notify(_ context.Context, level Level, text string) {
	if level < Warn {
		return
	}
	msg := text
	if d.authorID != "" {
		msg = "<@" + d.authorID + "> " + text
	}
	if _, err := d.session.ChannelMessageSend(d.channelID, msg); err != nil {
		d.logger.Warn("discord channel alert failed", "err", err, "channel", d.channelID)
	}
}
