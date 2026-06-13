# OwO Farm Bot

OwO Discord botu için komutları otomatik çalıştıran (hunt / battle / pray / gem /
weapon satışı) bir yardımcı bot. Captcha veya hata durumunda **Telegram'a push
bildirim** ve kanalda mention ile seni uyarır.

> ⚠️ Not: OwO otomasyonu Discord ve OwO kullanım şartlarına aykırıdır ve hesap
> banlanma riski taşır. Kendi sorumluluğunda kullan. Captcha tespiti tam da bu
> riski azaltmak içindir — captcha gelince bot **anında durur** ve seni uyarır.

## Nasıl çalışır

İki kimlik kullanır:

- **`BOT_TOKEN`** (gerçek bot) — kanalı dinler, durum/uyarı mesajı atar.
- **`BEARER_TOKEN`** (kullanıcı token'ın) — `owo h`, `owo b` gibi komutları senin
  adına gönderir.

OwO'nun yanıtlarını izler: captcha/ban → durup uyarır; hunt sonucu → envanter
kontrol eder; envanter → gemleri kullanır.

## Kurulum

```bash
cp .env.example .env       # ardından .env içindeki değerleri doldur
go build -o owobot ./cmd/bot
./owobot
```

Go 1.22+ gerekir. Gerekli ortam değişkenleri için `.env.example` dosyasına bak.
Telegram uyarıları için `TELEGRAM_BOT_TOKEN` + `TELEGRAM_CHAT_ID` doldur
(@BotFather'dan bot, @userinfobot'tan chat id).

## Kanal komutları

Kanala şunları yazarak botu kontrol edersin:

| Komut | Yapar |
|-------|-------|
| `owoh` | Farming'i başlatır |
| `owo fast` | Hızlı mod + başlatır |
| `owob fr` | Arkadaşla battle modu + başlatır |
| `dur` | Durdurur (👍) |
| `sa` | Captcha uyarısını susturur (canlılık testi) |
| `sell ww` | Weapon'ları satar |
| `ping` | Kullanıcı token'ını test eder |

## Captcha / hata olunca

| Olay | Aksiyon |
|------|---------|
| Captcha / verify / ban (kanalda) | Durur + Telegram + kanal mention + çözülene kadar tekrar uyarı |
| Kullanıcı token geçersiz (401/403) | Durur + Telegram kritik uyarı |
| Komutlar üst üste gönderilemiyor | Telegram uyarı |
| Discord 30+ sn kopuk | Telegram uyarı (discordgo otomatik yeniden bağlanır) |
| Rate limit (429) | Otomatik backoff + tekrar |

Captcha gelince botu çözüp `owoh` ile devam ettir, ya da `sa` ile sadece
uyarıyı sustur.

## Yapı

```
cmd/bot/          giriş noktası + wiring
internal/config/  ortam değişkeni yükleme & doğrulama
internal/alert/   Telegram / kanal mention / log bildirimleri
internal/owo/     OwO komut gönderimi + mesaj tespiti + envanter parse
internal/farm/    farming döngüsü, durum yönetimi, captcha tepkisi
internal/discord/ discordgo handler'ları + komut yönlendirme
```

## Geliştirme

```bash
go build ./...
go vet ./...
go test -race ./...
```
