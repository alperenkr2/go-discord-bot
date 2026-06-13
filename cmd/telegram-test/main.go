// Command telegram-test sends a single test message using the TELEGRAM_* values
// from your .env, printing the exact Telegram response so misconfigured tokens
// or chat ids are easy to diagnose.
//
// Usage:  go run ./cmd/telegram-test
package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"go-discord-bot/internal/alert"
	"go-discord-bot/internal/config"
)

func main() {
	cfg := config.Load()

	if !cfg.TelegramEnabled() {
		fmt.Println("TELEGRAM_BOT_TOKEN ve/veya TELEGRAM_CHAT_ID .env'de boş. Önce doldur.")
		os.Exit(1)
	}

	// Quiet logger; we print the result ourselves.
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tg := alert.NewTelegram(cfg.TelegramBotToken, cfg.TelegramChatID, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	fmt.Printf("Chat %s adresine test mesajı gönderiliyor...\n", cfg.TelegramChatID)
	if err := tg.Send(ctx, "✅ OwO bot Telegram testi — bu mesajı görüyorsan ayar doğru."); err != nil {
		fmt.Println("BAŞARISIZ:", err)
		fmt.Println()
		fmt.Println("Sık karşılaşılan sebepler:")
		fmt.Println("  • 'chat not found': Telegram'da botunla bir sohbet açıp /start göndermedin.")
		fmt.Println("    (BotFather'dan oluşturduğun botu bul, mesaj at, sonra tekrar dene.)")
		fmt.Println("  • Yanlış chat id: @userinfobot'a yaz, sana SENİN sayısal id'ni verir.")
		fmt.Println("    (Bot id'sini değil, kendi kullanıcı id'ni kullan.)")
		fmt.Println("  • Yanlış bot token: @BotFather > /mybots > API Token ile karşılaştır.")
		os.Exit(1)
	}

	fmt.Println("BAŞARILI — Telegram'a test mesajı gönderildi. Telefonuna düşmüş olmalı.")
}
