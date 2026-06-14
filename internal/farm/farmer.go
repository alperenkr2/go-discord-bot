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
	oneShotTimeout       = 30 * time.Second

	cratesPerOpen     = 20               // "owo wc all" opens up to 20 crates
	crateOpenCooldown = 30 * time.Second // OwO cooldown between "owo wc all" calls

	activeCheckInterval = 5 * time.Minute  // re-check while sleeping outside active hours
	quotaCheckInterval  = 10 * time.Minute // re-check while the daily hunt cap is hit
	humanMsgMinInterval = 50 * time.Second // floor between human-like chat messages
	breakJitterFraction = 0.30             // ± jitter applied to break durations

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
	OpenWeaponCrates(ctx context.Context) error
	SellWeapons(ctx context.Context) error
	Coinflip(ctx context.Context, amount int) error
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
	sellPending   bool               // next inventory drives the sell flow, not gem use
	huntsToday    int                // hunts performed today (for the daily limit)
	huntDay       int                // YearDay that huntsToday belongs to
	lastHuman     time.Time          // last human-message time (throttle)
	gambleDay     int                // YearDay the gamble counters belong to
	gamblesToday  int                // coinflips played today
	gambleNet     int                // net cowoncy from today's coinflips
	pendingBet    int                // bet awaiting a result (0 = none)
	gambleStopped bool               // loss-limit alert already sent today
	cancel        context.CancelFunc // cancels the farm loop
	captchaCancel context.CancelFunc // cancels the captcha reminder
	sellCancel    context.CancelFunc // cancels an in-progress sell/crate-open flow
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
	if f.sellCancel != nil {
		f.sellCancel()
		f.sellCancel = nil
	}
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

// StartSell begins the weapon sell flow: request the inventory, then (when the
// listing arrives in HandleOwO) open all crates and sell each rarity. OwO's
// per-sell confirmation button is tapped manually by the operator. Only one sell
// flow runs at a time.
func (f *Farmer) StartSell(channelID string) {
	f.mu.Lock()
	if f.sellPending || f.sellCancel != nil {
		f.mu.Unlock()
		f.reply("zaten bir satış işlemi sürüyor/bekliyor (durdurmak için 'dur').")
		return
	}
	f.channelID = channelID
	f.sellPending = true
	f.mu.Unlock()

	f.reply("envanter kontrol ediliyor (weapon crate sayısı için)...")
	f.runOneShot("sell: inventory", f.client.Inventory)
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
		if f.takeSellPending() {
			go f.runSell(content)
			return
		}
		if cmd, ok := owo.BuildUseCommand(content); ok {
			f.reply("gem bitmiş, takviye yapılıyor")
			f.runOneShot("use gems", func(ctx context.Context) error {
				return f.client.Send(ctx, cmd)
			})
		}
	case owo.IsCoinflipResult(content):
		f.resolveGamble(content)
	}
}

// HandleOwOUpdate reacts to an edited OwO message. OwO often delivers the captcha
// (and its escalating "(n/5)" counter) by editing a message, so the critical
// captcha/ban signals are re-checked on edits too — but not the gem/inventory
// flow, to avoid re-triggering it on every unrelated edit.
func (f *Farmer) HandleOwOUpdate(content string) {
	if owo.IsCaptcha(content) || owo.IsBan(content) {
		f.onCaptcha(content)
		return
	}
	if owo.IsCoinflipResult(content) {
		f.resolveGamble(content)
	}
}

func (f *Farmer) run(ctx context.Context) {
	f.logger.Info("farm loop started")
	defer f.logger.Info("farm loop stopped")

	for i := 0; ; i++ {
		// Sleep through inactive hours and respect the daily hunt cap — running
		// 24/7 at a steady pace is the biggest captcha trigger.
		if !f.waitForActiveHours(ctx) {
			return
		}
		if !f.waitForDailyQuota(ctx) {
			return
		}

		fast, friend := f.snapshot()

		delay := f.delay(fast)
		if f.cfg.CoverMessage {
			f.sendHumanMessage(ctx)
		}
		if !sleepCtx(ctx, delay) {
			return
		}

		if f.do(ctx, f.client.Hunt) {
			return
		}
		f.recordHunt()
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
			f.maybeGamble(ctx)
			if !sleepCtx(ctx, f.breakDuration(fast)) {
				return
			}
		}

		// Occasional longer "stepped away" break — humanizes the rhythm.
		if f.cfg.LongBreakEvery > 0 && (i+1)%f.cfg.LongBreakEvery == 0 {
			d := f.randomLongBreak()
			f.logger.Info("taking a long break", "duration", d)
			f.reply("biraz ara veriyorum, birazdan dönerim")
			if !sleepCtx(ctx, d) {
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

// runSell opens every weapon crate (looping "owo wc all" with OwO's cooldown,
// sized from the inventory count) and then sells each rarity tier.
func (f *Farmer) runSell(inv string) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.setSellCancel(cancel)
	defer f.clearSellCancel()

	f.logger.Debug("sell: inventory received", "text", inv)

	if n, ok := owo.ParseWeaponCrates(inv); ok && n > 0 {
		loops := (n + cratesPerOpen - 1) / cratesPerOpen // ceil(n / 20)
		eta := time.Duration(loops-1) * crateOpenCooldown
		f.reply(fmt.Sprintf("%d weapon crate var, %d kez 'owo wc all' açılıyor (~%s)...", n, loops, eta))

		for i := 0; i < loops; i++ {
			if err := f.client.OpenWeaponCrates(ctx); err != nil && f.sellSendFatal(err) {
				return
			}
			if i < loops-1 && !sleepCtx(ctx, crateOpenCooldown) {
				f.reply("kutu açma durduruldu")
				return
			}
		}
	} else {
		f.logger.Info("sell: envanterde weapon crate (kod 100) bulunamadı", "inventory", truncate(inv, 500))
		f.reply("inv'de weapon crate görünmüyor; direkt satışa geçiliyor")
	}

	if err := f.client.SellWeapons(ctx); err != nil && f.sellSendFatal(err) {
		return
	}
	f.reply("weapon satışı gönderildi — OwO'daki onay butonlarına basman gerekebilir")
}

// sellSendFatal reports whether the sell flow should abort after err.
func (f *Farmer) sellSendFatal(err error) bool {
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return true
	case errors.Is(err, owo.ErrUnauthorized):
		f.logger.Error("owo unauthorized during sell", "err", err)
		f.alert(alert.Critical, "Komut gönderilemiyor: kullanıcı token geçersiz olabilir.")
		return true
	default:
		f.logger.Warn("sell flow send failed", "err", err)
		return false
	}
}

func (f *Farmer) takeSellPending() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sellPending {
		f.sellPending = false
		return true
	}
	return false
}

func (f *Farmer) setSellCancel(cancel context.CancelFunc) {
	f.mu.Lock()
	f.sellCancel = cancel
	f.mu.Unlock()
}

func (f *Farmer) clearSellCancel() {
	f.mu.Lock()
	f.sellCancel = nil
	f.mu.Unlock()
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
	return jitter(f.cfg.BreakDuration, breakJitterFraction)
}

func (f *Farmer) randomLongBreak() time.Duration {
	min, max := f.cfg.LongBreakMin, f.cfg.LongBreakMax
	if max <= min {
		return min
	}
	return min + time.Duration(rand.Int64N(int64(max-min)))
}

// waitForActiveHours blocks until the current time is inside the active window,
// re-checking periodically. Returns false if the loop is cancelled.
func (f *Farmer) waitForActiveHours(ctx context.Context) bool {
	announced := false
	for !f.inActiveWindow(time.Now()) {
		if !announced {
			f.logger.Info("outside active hours — pausing", "start", f.cfg.ActiveStartHour, "end", f.cfg.ActiveEndHour)
			announced = true
		}
		if !sleepCtx(ctx, activeCheckInterval) {
			return false
		}
	}
	return true
}

func (f *Farmer) inActiveWindow(t time.Time) bool {
	s, e := f.cfg.ActiveStartHour, f.cfg.ActiveEndHour
	if s == e {
		return true // active all day
	}
	h := t.Hour()
	if s < e {
		return h >= s && h < e
	}
	return h >= s || h < e // window wraps past midnight
}

// waitForDailyQuota blocks while today's hunt cap is hit, until the day rolls
// over. Returns false if the loop is cancelled.
func (f *Farmer) waitForDailyQuota(ctx context.Context) bool {
	if f.cfg.DailyHuntLimit <= 0 {
		return true
	}
	announced := false
	for f.quotaReached() {
		if !announced {
			f.logger.Info("daily hunt limit reached — pausing until tomorrow", "limit", f.cfg.DailyHuntLimit)
			announced = true
		}
		if !sleepCtx(ctx, quotaCheckInterval) {
			return false
		}
	}
	return true
}

// quotaReached resets the counter on a new day, then reports whether the cap is hit.
func (f *Farmer) quotaReached() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rolloverLocked()
	return f.huntsToday >= f.cfg.DailyHuntLimit
}

func (f *Farmer) recordHunt() {
	f.mu.Lock()
	f.rolloverLocked()
	f.huntsToday++
	f.mu.Unlock()
}

func (f *Farmer) rolloverLocked() {
	if day := time.Now().YearDay(); day != f.huntDay {
		f.huntDay = day
		f.huntsToday = 0
	}
}

// maybeGamble plays one coinflip during a break, if enabled and under both the
// per-day count cap (the hard safety) and the daily net-loss limit. Coinflip is
// negative expected value — these caps bound the damage.
func (f *Farmer) maybeGamble(ctx context.Context) {
	if !f.cfg.GambleEnabled || f.cfg.GambleMaxPerDay <= 0 {
		return
	}

	f.mu.Lock()
	f.gambleRolloverLocked()
	overLoss := f.cfg.GambleDailyLossLimit > 0 && f.gambleNet <= -f.cfg.GambleDailyLossLimit
	if f.gamblesToday >= f.cfg.GambleMaxPerDay || overLoss || f.pendingBet != 0 {
		f.mu.Unlock()
		return
	}
	bet := f.cfg.GambleBetMin
	if span := f.cfg.GambleBetMax - f.cfg.GambleBetMin; span > 0 {
		bet += rand.IntN(span + 1)
	}
	f.gamblesToday++
	f.pendingBet = bet
	f.mu.Unlock()

	f.logger.Info("coinflip bet", "amount", bet)
	if err := f.client.Coinflip(ctx, bet); err != nil {
		f.logger.Warn("coinflip send failed", "err", err)
		f.mu.Lock()
		f.pendingBet = 0
		f.mu.Unlock()
	}
}

// resolveGamble applies a coinflip win/loss to today's net. It is gated on a
// pending bet, so unrelated messages are ignored.
func (f *Farmer) resolveGamble(content string) {
	f.mu.Lock()
	bet := f.pendingBet
	if bet == 0 {
		f.mu.Unlock()
		return
	}
	f.pendingBet = 0
	won := owo.IsCoinflipWin(content)
	if won {
		f.gambleNet += bet
	} else {
		f.gambleNet -= bet
	}
	net := f.gambleNet
	hitLimit := f.cfg.GambleDailyLossLimit > 0 && net <= -f.cfg.GambleDailyLossLimit && !f.gambleStopped
	if hitLimit {
		f.gambleStopped = true
	}
	f.mu.Unlock()

	f.logger.Debug("coinflip raw result", "content", truncate(content, 200))
	f.logger.Info("coinflip result", "won", won, "bet", bet, "net_today", net)
	if won {
		f.reply(fmt.Sprintf("🪙 kazandın +%d  (bugün net: %d)", bet, net))
	} else {
		f.reply(fmt.Sprintf("🪙 kaybettin -%d  (bugün net: %d)", bet, net))
	}
	if hitLimit {
		f.alert(alert.Warn, fmt.Sprintf("Coinflip günlük kayıp limiti aşıldı (bugün net: %d cowoncy). Kumar bugünlük durduruldu.", net))
	}
}

// GambleOnce plays a single coinflip immediately (a manual test command). It
// ignores the GambleEnabled flag and the daily caps — it's a deliberate one-off.
func (f *Farmer) GambleOnce() {
	f.mu.Lock()
	if f.pendingBet != 0 {
		f.mu.Unlock()
		f.reply("önceki coinflip sonucu bekleniyor, birazdan tekrar dene")
		return
	}
	bet := f.cfg.GambleBetMin
	if span := f.cfg.GambleBetMax - f.cfg.GambleBetMin; span > 0 {
		bet += rand.IntN(span + 1)
	}
	if bet <= 0 {
		bet = 50000
	}
	f.pendingBet = bet
	f.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), oneShotTimeout)
	defer cancel()
	f.logger.Info("manual coinflip test", "amount", bet)
	f.reply(fmt.Sprintf("🪙 test coinflip: %d bahis gönderiliyor...", bet))
	if err := f.client.Coinflip(ctx, bet); err != nil {
		f.logger.Warn("coinflip send failed", "err", err)
		f.reply("coinflip gönderilemedi (token/ağ?)")
		f.mu.Lock()
		f.pendingBet = 0
		f.mu.Unlock()
	}
}

func (f *Farmer) gambleRolloverLocked() {
	if day := time.Now().YearDay(); day != f.gambleDay {
		f.gambleDay = day
		f.gamblesToday = 0
		f.gambleNet = 0
		f.gambleStopped = false
		f.pendingBet = 0
	}
}

// sendHumanMessage posts a varied message from the pool (humanize + maybe XP),
// throttled so it never spams faster than once per humanMsgMinInterval.
func (f *Farmer) sendHumanMessage(ctx context.Context) {
	pool := f.cfg.HumanMessages
	if len(pool) == 0 {
		return
	}
	f.mu.Lock()
	if !f.lastHuman.IsZero() && time.Since(f.lastHuman) < humanMsgMinInterval {
		f.mu.Unlock()
		return
	}
	f.lastHuman = time.Now()
	f.mu.Unlock()

	if err := f.client.Send(ctx, pool[rand.IntN(len(pool))]); err != nil {
		f.logger.Warn("human message failed", "err", err)
	}
}

// jitter returns d scaled by a random factor in [1-frac, 1+frac].
func jitter(d time.Duration, frac float64) time.Duration {
	if d <= 0 {
		return d
	}
	return time.Duration(float64(d) * (1 + (rand.Float64()*2-1)*frac))
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
