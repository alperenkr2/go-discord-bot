package owo

import (
	"fmt"
	"regexp"
	"strings"
)

// gemSlots are the inventory gem tiers the bot auto-uses, in OwO's ordering.
// Slot 2 is intentionally skipped to match the original behaviour.
var gemSlots = []int{1, 3, 4}

var gemRegexps = buildGemRegexps()

func buildGemRegexps() map[int]*regexp.Regexp {
	m := make(map[int]*regexp.Regexp, len(gemSlots))
	for _, i := range gemSlots {
		m[i] = regexp.MustCompile(fmt.Sprintf("(\\d+)`<:(?:c|u|l|r|e|m|f)?gem%d:\\d+>", i))
	}
	return m
}

// BuildUseCommand parses an OwO inventory listing and returns the "owo use ..."
// command that spends one gem from each tracked tier. The bool is false when no
// usable gems were found, so the caller can skip sending an empty command.
func BuildUseCommand(inventory string) (string, bool) {
	text := "owo use "
	found := false

	for _, i := range gemSlots {
		matches := gemRegexps[i].FindAllStringSubmatch(inventory, -1)

		var result string
		for _, match := range matches {
			result += strings.ReplaceAll(match[1], "0", "") + " "
		}

		nums := strings.Split(result, " ")
		if len(nums) < 2 {
			continue
		}

		text += " " + nums[len(nums)-2]
		found = true
	}

	return text, found
}
