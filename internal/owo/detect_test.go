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
