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
