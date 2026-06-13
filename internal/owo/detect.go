// Package owo talks to the OwO bot: it sends commands as the user and classifies
// OwO's replies (captcha, ban, hunt result, inventory).
package owo

import (
	"strings"
	"unicode"
)

// stripInvisible removes zero-width / invisible format characters. OwO injects
// characters like U+200B (zero-width space) *inside* words — e.g. "c​aptcha",
// "ver​ify", "hu​man" — specifically to defeat naive substring matching.
// Dropping every Unicode "Cf" (format) rune undoes that without touching
// visible text.
func stripInvisible(content string) string {
	var b strings.Builder
	b.Grow(len(content))
	for _, r := range content {
		if unicode.Is(unicode.Cf, r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// normalize strips invisible characters and lowercases, for case-insensitive
// keyword matching.
func normalize(content string) string {
	return strings.ToLower(stripInvisible(content))
}

// IsCaptcha reports whether content looks like an OwO captcha / verification
// prompt. It matches on the strong signals ("captcha", or a "verify ... human"
// pairing) after normalization, so zero-width-space evasion does not slip past.
func IsCaptcha(content string) bool {
	c := normalize(content)
	switch {
	case strings.Contains(c, "captcha"):
		return true
	case strings.Contains(c, "owobot.com/captcha"):
		return true
	case strings.Contains(c, "verify") && strings.Contains(c, "human"):
		return true
	case strings.Contains(c, "are you a real human"):
		return true
	default:
		return false
	}
}

// IsBan reports whether content looks like an OwO ban / blacklist notice.
func IsBan(content string) bool {
	c := normalize(content)
	return strings.Contains(c, "banned") || strings.Contains(c, "blacklist")
}

// IsCaught reports whether content is a hunt result (triggers an inventory check).
func IsCaught(content string) bool {
	return strings.Contains(normalize(content), "caught")
}

// IsInventory reports whether content is an OwO inventory listing. It keeps the
// "Inventory" header's capitalization (after stripping invisible characters) to
// avoid matching the lowercase word in ordinary chatter.
func IsInventory(content string) bool {
	return strings.Contains(stripInvisible(content), "Inventory")
}
