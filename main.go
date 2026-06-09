package main

import (
	"encoding/base64"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

func main() {
	var (
		link   = flag.String("link", "", "یک کانفیگ تکی: vmess:// | vless:// | trojan:// | ss://")
		sub    = flag.String("sub", "", "آدرس ساب‌اسکریپشن (URL)")
		index  = flag.Int("index", 0, "وقتی از --sub استفاده می‌کنید، شماره کانفیگ موردنظر (از 0)")
		listen = flag.String("listen", "127.0.0.1", "آی‌پی محل اجرای پروکسی")
		socks  = flag.Int("socks", 10808, "پورت SOCKS5")
		httpP  = flag.Int("http", 0, "پورت HTTP proxy (0 یعنی غیرفعال)")
		list   = flag.Bool("list", false, "فقط لیست کانفیگ‌های ساب را نشان بده و خارج شو")
	)
	flag.Parse()

	// اگر هیچ کانفیگی از خط‌فرمان داده نشده باشد، حالت گرافیکی اجرا می‌شود.
	if *link == "" && *sub == "" {
		runGUI()
		return
	}
	runCLI(*link, *sub, *index, *listen, *socks, *httpP, *list)
}

func runCLI(link, sub string, index int, listen string, socks, httpP int, list bool) {
	var rawLinks []string
	if link != "" {
		rawLinks = []string{link}
	} else {
		ls, err := fetchSub(sub)
		if err != nil {
			fatal("خطا در دریافت ساب: %v", err)
		}
		rawLinks = ls
	}
	if len(rawLinks) == 0 {
		fatal("هیچ کانفیگی پیدا نشد.")
	}

	if list {
		for i, l := range rawLinks {
			fmt.Printf("[%d] %s\n", i, summarize(l))
		}
		return
	}
	if index < 0 || index >= len(rawLinks) {
		fatal("شماره کانفیگ نامعتبر است (0..%d).", len(rawLinks)-1)
	}
	chosen := rawLinks[index]

	eng := &Engine{}
	if err := eng.Start(chosen, listen, socks, httpP); err != nil {
		fatal("%v", err)
	}
	defer eng.Stop()

	fmt.Printf("✅ پروکسی فعال شد\n")
	fmt.Printf("   کانفیگ : %s\n", summarize(chosen))
	fmt.Printf("   SOCKS5 : %s:%d\n", listen, socks)
	if httpP > 0 {
		fmt.Printf("   HTTP   : %s:%d\n", listen, httpP)
	}
	fmt.Printf("\nبرای تست:\n  curl --socks5-hostname %s:%d https://api.ipify.org\n", listen, socks)
	fmt.Printf("\nبرای توقف Ctrl+C را بزنید.\n")

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	fmt.Println("\nدر حال خاموش‌شدن...")
}

// fetchSub آدرس ساب را گرفته، در صورت base64 بودن دیکد می‌کند و لینک‌ها را برمی‌گرداند.
func fetchSub(url string) ([]string, error) {
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	text := strings.TrimSpace(string(body))

	if decoded, err := tryBase64(text); err == nil {
		text = decoded
	}

	var out []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.Contains(line, "://") {
			out = append(out, line)
		}
	}
	return out, nil
}

func tryBase64(s string) (string, error) {
	s = strings.TrimSpace(s)
	if strings.Contains(s, "://") {
		return "", fmt.Errorf("not base64")
	}
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if b, err := enc.DecodeString(strings.TrimSpace(s)); err == nil {
			return string(b), nil
		}
	}
	return "", fmt.Errorf("not base64")
}

func summarize(link string) string {
	if len(link) > 60 {
		return link[:57] + "..."
	}
	return link
}

func fatal(format string, a ...interface{}) {
	fmt.Fprintf(os.Stderr, "❌ "+format+"\n", a...)
	os.Exit(1)
}
