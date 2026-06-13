// Package owo talks to the OwO bot: it sends commands as the user and classifies
// OwO's replies (captcha, ban, hunt result, inventory).
package owo

import "strings"

// IsCaptcha reports whether content looks like an OwO captcha / verification
// prompt. OwO phrases these a few different ways and may deliver them in a DM,
// so we match case-insensitively on the strong signals ("captcha", or a
// "verify ... human" pairing) instead of the old single literal "captcha".
func IsCaptcha(content string) bool {
	c := strings.ToLower(content)
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
	c := strings.ToLower(content)
	return strings.Contains(c, "banned") || strings.Contains(c, "blacklist")
}

// IsCaught reports whether content is a hunt result (triggers an inventory check).
func IsCaught(content string) bool {
	return strings.Contains(strings.ToLower(content), "caught")
}

// IsInventory reports whether content is an OwO inventory listing.
func IsInventory(content string) bool {
	return strings.Contains(content, "Inventory")
}
