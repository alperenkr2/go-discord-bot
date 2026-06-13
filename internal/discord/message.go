package discord

import (
	"strings"

	"github.com/bwmarrin/discordgo"
)

// extractText flattens every text source in a message into one string: the plain
// content, embed text, and — for Components V2 messages, where Content is empty —
// the text inside TextDisplay/Section/Container/ActionsRow components. OwO sends
// many of its messages (hunt results, captcha, inventory) as Components V2, so
// detection must look past m.Content.
func extractText(m *discordgo.Message) string {
	var b strings.Builder
	appendText(&b, m.Content)

	for _, e := range m.Embeds {
		if e == nil {
			continue
		}
		appendText(&b, e.Title)
		appendText(&b, e.Description)
		if e.Author != nil {
			appendText(&b, e.Author.Name)
		}
		if e.Footer != nil {
			appendText(&b, e.Footer.Text)
		}
		for _, f := range e.Fields {
			if f == nil {
				continue
			}
			appendText(&b, f.Name)
			appendText(&b, f.Value)
		}
	}

	for _, c := range m.Components {
		appendComponentText(&b, c)
	}
	return b.String()
}

func appendComponentText(b *strings.Builder, c discordgo.MessageComponent) {
	switch v := c.(type) {
	case *discordgo.TextDisplay:
		appendText(b, v.Content)
	case *discordgo.Section:
		for _, sub := range v.Components {
			appendComponentText(b, sub)
		}
		appendComponentText(b, v.Accessory)
	case *discordgo.Container:
		for _, sub := range v.Components {
			appendComponentText(b, sub)
		}
	case *discordgo.ActionsRow:
		for _, sub := range v.Components {
			appendComponentText(b, sub)
		}
	case *discordgo.Button:
		appendText(b, v.Label)
	}
}

func appendText(b *strings.Builder, s string) {
	if s == "" {
		return
	}
	if b.Len() > 0 {
		b.WriteByte('\n')
	}
	b.WriteString(s)
}
