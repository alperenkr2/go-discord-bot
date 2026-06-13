package owo

import (
	"fmt"
	"regexp"
	"strconv"
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

// OwO inventory items render as `CODE`<:emoji:id>QTY, where CODE is the item's
// fixed catalog id (zero-padded) and QTY is the quantity in superscript digits.
// The weapon crate is catalog id 100, e.g. `100`<a:wc:123>⁵⁷⁴ means 574 crates.
const weaponCrateCode = "100"

var weaponCrateRe = regexp.MustCompile("`" + weaponCrateCode + "`<a?:[A-Za-z0-9_]+:\\d+>([⁰¹²³⁴⁵⁶⁷⁸⁹]+)")

var superscriptDigit = map[rune]rune{
	'⁰': '0', '¹': '1', '²': '2', '³': '3', '⁴': '4',
	'⁵': '5', '⁶': '6', '⁷': '7', '⁸': '8', '⁹': '9',
}

// ParseWeaponCrates returns how many weapon crates (catalog id 100) the inventory
// lists. The bool is false when no weapon-crate entry is found.
func ParseWeaponCrates(inventory string) (int, bool) {
	m := weaponCrateRe.FindStringSubmatch(inventory)
	if len(m) != 2 {
		return 0, false
	}
	return superscriptToInt(m[1])
}

// superscriptToInt converts a run of superscript digits (e.g. "⁵⁷⁴") to an int.
func superscriptToInt(s string) (int, bool) {
	var b strings.Builder
	for _, r := range s {
		d, ok := superscriptDigit[r]
		if !ok {
			return 0, false
		}
		b.WriteRune(d)
	}
	n, err := strconv.Atoi(b.String())
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}
