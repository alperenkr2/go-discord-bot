package owo

import "testing"

func TestIsCaptcha(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{"literal captcha", "Please complete the captcha to continue", true},
		{"captcha url", "Verify here: https://owobot.com/captcha?a=1", true},
		{"verify + human DM", "Please verify that you are a human!", true},
		{"real human phrasing", "Are you a real human? Click the link.", true},
		{"uppercase", "CAPTCHA DETECTED", true},
		{"real owo captcha", "⚠️ | <@123456789012345678>! Please complete your captcha to verify that you are human! (2/5)", true},
		{"zero-width-space evasion", "⚠️ | <@123>! Pl​ease c​omplete yo​ur c​aptcha t​o ver​ify th​at y​ou a​re hu​man! (2/5)", true},
		{"normal hunt result", "**alper** caught a common **dog**!", false},
		{"lone human word", "haha you are such a human being", false},
		{"command echo", "owo h", false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsCaptcha(tt.content); got != tt.want {
				t.Errorf("IsCaptcha(%q) = %v, want %v", tt.content, got, tt.want)
			}
		})
	}
}

func TestIsBan(t *testing.T) {
	tests := []struct {
		content string
		want    bool
	}{
		{"You have been banned from using OwO", true},
		{"Your account is blacklisted", true},
		{"**alper** caught a dog", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := IsBan(tt.content); got != tt.want {
			t.Errorf("IsBan(%q) = %v, want %v", tt.content, got, tt.want)
		}
	}
}

func TestIsCaught(t *testing.T) {
	tests := []struct {
		content string
		want    bool
	}{
		{"you caught a **rabbit**", true},
		{"Caught something!", true},
		{"nothing happened", false},
	}
	for _, tt := range tests {
		if got := IsCaught(tt.content); got != tt.want {
			t.Errorf("IsCaught(%q) = %v, want %v", tt.content, got, tt.want)
		}
	}
}

func TestIsInventory(t *testing.T) {
	if !IsInventory("__**alper's Inventory**__") {
		t.Error("expected Inventory header to be detected")
	}
	if IsInventory("just some lowercase inventory text") {
		t.Error("did not expect lowercase 'inventory' to match")
	}
}

func TestBuildUseCommand(t *testing.T) {
	inv := "12`<:cgem1:111> some text 34`<:ugem3:222> more 45`<:lgem4:333>"
	got, ok := BuildUseCommand(inv)
	if !ok {
		t.Fatalf("BuildUseCommand returned ok=false for a populated inventory")
	}
	want := "owo use  12 34 45"
	if got != want {
		t.Errorf("BuildUseCommand() = %q, want %q", got, want)
	}
}

func TestBuildUseCommandNoGems(t *testing.T) {
	if _, ok := BuildUseCommand("an inventory with no gems at all"); ok {
		t.Error("expected ok=false when no gems are present")
	}
}

func TestCoinflip(t *testing.T) {
	win := "**SERDAR ORTAC** spent **<:cowoncy:416043450337853441> 50,000** and chose **heads**\n" +
		"The coin spins... <:head:436677933977960478> and you won **<:cowoncy:416043450337853441> 100,000**!!"
	loss := "**SERDAR ORTAC** spent **<:cowoncy:416043450337853441> 20,000** and chose **heads**\n" +
		"The coin spins... <:tail:436677926398853120> and you lost it all... :c"

	if !IsCoinflipResult(win) {
		t.Error("win message should be detected as a coinflip result")
	}
	if !IsCoinflipResult(loss) {
		t.Error("loss message should be detected as a coinflip result")
	}
	if !IsCoinflipWin(win) {
		t.Error("win message should be a win")
	}
	if IsCoinflipWin(loss) {
		t.Error("loss message should not be a win")
	}
	if IsCoinflipResult("**alper** caught a common **dog**!") {
		t.Error("a hunt result must not be mistaken for a coinflip")
	}

	// OwO posts this first, then edits it to add the outcome. The placeholder
	// must NOT be treated as a settled result (the original bug: it has no "lost"
	// yet, so it mis-read as a win).
	spinning := "**SERDAR ORTAC** spent **<:cowoncy:1> 88,303** and chose **heads**\n" +
		"The coin spins... <a:coin:2>"
	if IsCoinflipResult(spinning) {
		t.Error("the pre-result 'coin spins...' placeholder must not be a settled result")
	}
}

func TestParseWeaponCrates(t *testing.T) {
	// Real OwO inventory format: `CODE`<:emoji:id> then quantity in superscript.
	inv := "`056`<:mgem1:492572122590478356>⁰⁰⁹    `100`<a:weaponcrate:725570544065445919>⁵⁷⁴    `101`<:box:427352600476647425>⁰⁰⁶"
	if n, ok := ParseWeaponCrates(inv); !ok || n != 574 {
		t.Errorf("ParseWeaponCrates() = (%d, %v), want (574, true)", n, ok)
	}
	if _, ok := ParseWeaponCrates("`051`<:cgem1:492572122120585240>⁰⁷⁵"); ok {
		t.Error("expected ok=false when item 100 is absent")
	}
}
