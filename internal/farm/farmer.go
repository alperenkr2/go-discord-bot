// Package farm runs the OwO farming loop and reacts to OwO's messages. It owns
// all the bot's mutable runtime state behind a mutex and drives the loop through
// a cancellable context so it can be stopped instantly (by the user or a captcha).
package farm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"

	"go-discord-bot/internal/alert"
	"go-discord-bot/internal/config"
	"go-discord-bot/internal/owo"
)

const (
	sendFailureThreshold = 3
	coverTextLen         = 10
	coverCharset         = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	oneShotTimeout       = 30 * time.Second

	captchaMsg         = "CAPTCHA/BAN algılandı! OwO farming durduruldu. Captcha'yı çöz, sonra kanala 'sa' (sustur) veya 'owoh' (devam) yaz."
	captchaReminderMsg = "CAPTCHA hâlâ bekliyor — farming durmuş durumda. Çözünce 'owoh' ile devam et, 'dur' ile tamamen durdur."
)

// commander is the subset of *owo.Client the farmer needs. It is an interface so
// the loop can be unit-tested without real network calls.
type commander interface {
	Send(ctx context.Context, content string) error
	Hunt(ctx context.Context) error
	Battle(ctx context.Context, friendID string) error
	Pray(ctx context.Context) error
	Inventory(ctx context.Context) error
}

// messenger is the subset of *discordgo.Session used to post status replies.
type messenger interface {
	ChannelMessageSend(channelID, content string, options ...discordgo.RequestOption) (*discordgo.Message, error)
}

// Farmer coordinates the farming loop and OwO message reactions.
type Farmer struct {
	cfg     *config.Config
	client  commander
	notify  alert.Notifier
	session messenger
	logger  *slog.Logger

	mu            sync.Mutex
	running       bool
	fastMode      bool
	battleFriends bool
	channelID     string
	sendFailures  int
	cancel        context.CancelFunc // cancels the farm loop
	captchaCancel context.CancelFunc // cancels the captcha reminder
}

func New(cfg *config.Config, client commander, notify alert.Notifier, session messenger, logger *slog.Logger) *Farmer {
	return &Farmer{
		cfg:     cfg,
		client:  client,
		notify:  notify,
		session: session,
		logger:  logger,
	}
}

// Start begins farming in channelID. It is idempotent: calling it while already
// running just updates the channel. Starting always clears a pending captcha
// reminder, since the operator is signalling they want to continue.
func (f *Farmer) Start(channelID string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.clearCaptchaReminderLocked()
	f.channelID = channelID
	if f.running {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	f.cancel = cancel
	f.running = true
	go f.run(ctx)
}

// Stop halts the farm loop and silences any captcha reminder (the "dur" command).
func (f *Farmer) Stop() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopLoopLocked()
	f.clearCaptchaReminderLocked()
}

// ClearCaptcha silences the captcha reminder without resuming farming ("sa").
func (f *Farmer) ClearCaptcha() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.clearCaptchaReminderLocked()
}

// SetFast toggles fast mode (fixed short delays).
func (f *Farmer) SetFast(v bool) {
	f.mu.Lock()
	f.fastMode = v
	f.mu.Unlock()
}

// SetBattleFriends toggles battling the configured friend.
func (f *Farmer) SetBattleFriends(v bool) {
	f.mu.Lock()
	f.battleFriends = v
	f.mu.Unlock()
}

// HandleOwO reacts to a message from OwO (or anyone): captcha/ban stops the bot
// and alerts; a hunt result triggers an inventory check; an inventory listing
// triggers gem usage.
func (f *Farmer) HandleOwO(content string) {
	switch {
	case owo.IsCaptcha(content) || owo.IsBan(content):
		f.onCaptcha(content)
	case owo.IsCaught(content):
		f.runOneShot("inventory check", f.client.Inventory)
	case owo.IsInventory(content):
		if cmd, ok := owo.BuildUseCommand(content); ok {
			f.reply("gem bitmiş, takviye yapılıyor")
			f.runOneShot("use gems", func(ctx context.Context) error {
				return f.client.Send(ctx, cmd)
			})
		}
	}
}

func (f *Farmer) run(ctx context.Context) {
	f.logger.Info("farm loop started")
	defer f.logger.Info("farm loop stopped")

	for i := 0; ; i++ {
		fast, friend := f.snapshot()

		delay := f.delay(fast)
		if f.cfg.CoverMessage {
			f.sendCover(ctx, delay)
		}
		if !sleepCtx(ctx, delay) {
			return
		}

		if f.do(ctx, f.client.Hunt) {
			return
		}
		if !sleepCtx(ctx, time.Second) {
			return
		}

		if f.do(ctx, func(c context.Context) error { return f.client.Battle(c, friend) }) {
			return
		}
		if !sleepCtx(ctx, time.Second) {
			return
		}

		if (i+1)%f.cfg.BreakEvery == 0 {
			if !sleepCtx(ctx, 2*time.Second) {
				return
			}
			if f.do(ctx, f.client.Pray) {
				return
			}
			f.reply(fmt.Sprintf("%d kere çalıştım, azıcık mola veriyorum", i+1))
			if !sleepCtx(ctx, f.breakDuration(fast)) {
				return
			}
		}
	}
}

// do runs a farm command, returning true when the loop should stop.
func (f *Farmer) do(ctx context.Context, action func(context.Context) error) (stop bool) {
	if err := action(ctx); err != nil {
		return f.onSendError(err)
	}
	f.resetFailures()
	return false
}

func (f *Farmer) onSendError(err error) (stop bool) {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	if errors.Is(err, owo.ErrUnauthorized) {
		f.logger.Error("owo unauthorized — stopping farm", "err", err)
		f.alert(alert.Critical, "Komut gönderilemiyor: kullanıcı token (BEARER_TOKEN) geçersiz olabilir. Farming durduruldu.")
		f.stopLoop()
		return true
	}

	f.mu.Lock()
	f.sendFailures++
	n := f.sendFailures
	f.mu.Unlock()

	f.logger.Warn("owo send failed", "err", err, "consecutive", n)
	if n == sendFailureThreshold {
		f.alert(alert.Warn, fmt.Sprintf("OwO komutları üst üste %d kez gönderilemedi — ağ veya Discord sorunu olabilir.", n))
	}
	return false
}

// runOneShot executes a single OwO command outside the farm loop (e.g. from a
// message handler), with its own timeout.
func (f *Farmer) runOneShot(label string, action func(context.Context) error) {
	ctx, cancel := context.WithTimeout(context.Background(), oneShotTimeout)
	defer cancel()

	err := action(ctx)
	if err == nil {
		return
	}
	if errors.Is(err, owo.ErrUnauthorized) {
		f.logger.Error("owo unauthorized during "+label, "err", err)
		f.alert(alert.Critical, "Komut gönderilemiyor: kullanıcı token geçersiz olabilir. Farming durduruldu.")
		f.stopLoop()
		return
	}
	f.logger.Warn("owo "+label+" failed", "err", err)
}

func (f *Farmer) onCaptcha(content string) {
	f.logger.Warn("captcha/ban detected — stopping farm", "snippet", truncate(content, 160))
	f.stopLoop()
	f.startCaptchaReminder()
}

// startCaptchaReminder alerts immediately and then re-alerts on an interval until
// the operator resumes ("owoh") or clears it ("sa"/"dur").
func (f *Farmer) startCaptchaReminder() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.captchaCancel != nil {
		return // already reminding
	}
	ctx, cancel := context.WithCancel(context.Background())
	f.captchaCancel = cancel
	go f.remindCaptcha(ctx)
}

func (f *Farmer) remindCaptcha(ctx context.Context) {
	f.alert(alert.Critical, captchaMsg)

	ticker := time.NewTicker(f.cfg.CaptchaReminderEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			f.alert(alert.Critical, captchaReminderMsg)
		}
	}
}

// --- small helpers ---------------------------------------------------------

func (f *Farmer) stopLoop() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopLoopLocked()
}

func (f *Farmer) stopLoopLocked() {
	if f.cancel != nil {
		f.cancel()
		f.cancel = nil
	}
	f.running = false
}

func (f *Farmer) clearCaptchaReminderLocked() {
	if f.captchaCancel != nil {
		f.captchaCancel()
		f.captchaCancel = nil
	}
}

func (f *Farmer) snapshot() (fast bool, friend string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.battleFriends {
		friend = f.cfg.FriendID
	}
	return f.fastMode, friend
}

func (f *Farmer) resetFailures() {
	f.mu.Lock()
	f.sendFailures = 0
	f.mu.Unlock()
}

func (f *Farmer) delay(fast bool) time.Duration {
	if fast {
		return f.cfg.FastDelay
	}
	span := f.cfg.DelayMax - f.cfg.DelayMin
	if span <= 0 {
		return f.cfg.DelayMin
	}
	return f.cfg.DelayMin + time.Duration(rand.Int64N(int64(span)))
}

func (f *Farmer) breakDuration(fast bool) time.Duration {
	if fast {
		return f.cfg.FastDelay
	}
	return f.cfg.BreakDuration
}

func (f *Farmer) sendCover(ctx context.Context, d time.Duration) {
	text := fmt.Sprintf("%d sn cooldown. %s", int(d.Seconds()), randomText(coverTextLen))
	if err := f.client.Send(ctx, text); err != nil {
		f.logger.Warn("cover message failed", "err", err)
	}
}

func (f *Farmer) reply(text string) {
	f.mu.Lock()
	ch := f.channelID
	f.mu.Unlock()
	if ch == "" {
		return
	}
	if _, err := f.session.ChannelMessageSend(ch, text); err != nil {
		f.logger.Warn("channel reply failed", "err", err)
	}
}

func (f *Farmer) alert(level alert.Level, text string) {
	f.notify.Notify(context.Background(), level, text)
}

func randomText(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = coverCharset[rand.IntN(len(coverCharset))]
	}
	return string(b)
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
