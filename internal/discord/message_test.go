package discord

import (
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestExtractText(t *testing.T) {
	m := &discordgo.Message{
		Content: "plain hello",
		Embeds: []*discordgo.MessageEmbed{
			{Description: "embed body text"},
		},
		Components: []discordgo.MessageComponent{
			&discordgo.Container{
				Components: []discordgo.MessageComponent{
					&discordgo.TextDisplay{Content: "please complete the captcha"},
					&discordgo.ActionsRow{
						Components: []discordgo.MessageComponent{
							&discordgo.Button{Label: "Confirm"},
						},
					},
				},
			},
		},
	}

	got := extractText(m)
	for _, want := range []string{"plain hello", "embed body text", "captcha", "Confirm"} {
		if !strings.Contains(got, want) {
			t.Errorf("extractText() = %q, want it to contain %q", got, want)
		}
	}
}

func TestExtractTextEmpty(t *testing.T) {
	if got := extractText(&discordgo.Message{}); got != "" {
		t.Errorf("extractText(empty) = %q, want empty", got)
	}
}
