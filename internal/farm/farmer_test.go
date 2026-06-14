package farm

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"

	"go-discord-bot/internal/alert"
	"go-discord-bot/internal/config"
)

type fakeCommander struct{}

func (fakeCommander) Send(context.Context, string) error     { return nil }
func (fakeCommander) Hunt(context.Context) error             { return nil }
func (fakeCommander) Battle(context.Context, string) error   { return nil }
func (fakeCommander) Pray(context.Context) error             { return nil }
func (fakeCommander) Inventory(context.Context) error        { return nil }
func (fakeCommander) OpenWeaponCrates(context.Context) error { return nil }
func (fakeCommander) SellWeapons(context.Context) error      { return nil }
func (fakeCommander) Coinflip(context.Context, int) error    { return nil }

type fakeMessenger struct{}

func (fakeMessenger) ChannelMessageSend(_, _ string, _ ...discordgo.RequestOption) (*discordgo.Message, error) {
	return nil, nil
}

type nopNotifier struct{}

func (nopNotifier) Notify(context.Context, alert.Level, string) {}

// TestFarmerConcurrentControl hammers every state-mutating entry point from many
// goroutines at once. Run with -race to verify the mutex covers all shared state.
func TestFarmerConcurrentControl(t *testing.T) {
	cfg := &config.Config{
		DelayMin:             time.Millisecond,
		DelayMax:             2 * time.Millisecond,
		FastDelay:            time.Millisecond,
		BreakEvery:           4,
		BreakDuration:        time.Millisecond,
		CaptchaReminderEvery: time.Millisecond,
		CoverMessage:         true,
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	f := New(cfg, fakeCommander{}, nopNotifier{}, fakeMessenger{}, logger)

	var wg sync.WaitGroup
	deadline := time.Now().Add(300 * time.Millisecond)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for time.Now().Before(deadline) {
				f.Start("channel-123")
				f.SetFast(true)
				f.SetBattleFriends(true)
				f.HandleOwO("you caught a **rabbit**")
				f.HandleOwO("please complete the captcha")
				f.StartSell("channel-123")
				f.HandleOwO("__**Inventory**__ no crates here")
				f.ClearCaptcha()
				f.SetFast(false)
				f.Stop()
			}
		}()
	}
	wg.Wait()
	f.Stop()
}

func TestInActiveWindow(t *testing.T) {
	at := func(hour int) time.Time { return time.Date(2026, 1, 1, hour, 0, 0, 0, time.UTC) }
	tests := []struct {
		name       string
		start, end int
		hour       int
		want       bool
	}{
		{"wrap active after midnight", 10, 2, 1, true},
		{"wrap active daytime", 10, 2, 11, true},
		{"wrap inactive", 10, 2, 5, false},
		{"wrap start edge", 10, 2, 10, true},
		{"wrap end edge exclusive", 10, 2, 2, false},
		{"normal active", 9, 17, 12, true},
		{"normal inactive", 9, 17, 20, false},
		{"all day (start==end)", 0, 0, 3, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &Farmer{cfg: &config.Config{ActiveStartHour: tt.start, ActiveEndHour: tt.end}}
			if got := f.inActiveWindow(at(tt.hour)); got != tt.want {
				t.Errorf("inActiveWindow(start=%d, end=%d, hour=%d) = %v, want %v", tt.start, tt.end, tt.hour, got, tt.want)
			}
		})
	}
}

func TestJitter(t *testing.T) {
	base := 100 * time.Second
	for i := 0; i < 200; i++ {
		d := jitter(base, 0.30)
		if d < 70*time.Second || d > 130*time.Second {
			t.Fatalf("jitter(%v, 0.30) = %v, out of [70s,130s]", base, d)
		}
	}
	if jitter(0, 0.30) != 0 {
		t.Error("jitter(0) should be 0")
	}
}
