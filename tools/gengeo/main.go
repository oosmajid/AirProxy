package main

// gengeo فایل‌های geo سبک و مخصوص ایران را می‌سازد و در assets/geo/ می‌نویسد:
//   - geosite.dat : همان geosite-lite جامعه (فقط دستهٔ CATEGORY-IR)
//   - geoip.dat   : geoip استاندارد که فقط به IR + PRIVATE تریم شده است
//
// این ابزار را دستی اجرا کنید تا دیتای geo به‌روزرسانی شود:
//   go run ./tools/gengeo assets/geo
// خروجی در ریپو commit می‌شود؛ کاربر نهایی نیازی به اجرای آن ندارد.
//
// برای کار آفلاین، می‌توان به‌جای دانلود از فایل محلی خواند:
//   GEOSITE_SRC=/path/geosite-lite.dat GEOIP_SRC=/path/geoip.dat go run ./tools/gengeo assets/geo

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/xtls/xray-core/app/router"
	"google.golang.org/protobuf/proto"
)

const (
	geositeURL = "https://github.com/chocolate4u/Iran-v2ray-rules/releases/latest/download/geosite-lite.dat"
	geoipURL   = "https://github.com/chocolate4u/Iran-v2ray-rules/releases/latest/download/geoip.dat"
)

func main() {
	outDir := "assets/geo"
	if len(os.Args) > 1 {
		outDir = os.Args[1]
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		fatal(err)
	}

	fmt.Println("resolving geosite-lite …")
	site, err := loadSource("GEOSITE_SRC", geositeURL)
	if err != nil {
		fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "geosite.dat"), site, 0o644); err != nil {
		fatal(err)
	}
	fmt.Printf("  geosite.dat: %d bytes\n", len(site))

	fmt.Println("resolving geoip …")
	ipFull, err := loadSource("GEOIP_SRC", geoipURL)
	if err != nil {
		fatal(err)
	}
	trimmed, err := trimGeoIP(ipFull, []string{"IR", "PRIVATE"})
	if err != nil {
		fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "geoip.dat"), trimmed, 0o644); err != nil {
		fatal(err)
	}
	fmt.Printf("  geoip.dat: %d bytes (trimmed from %d)\n", len(trimmed), len(ipFull))
}

// trimGeoIP یک GeoIPList را می‌گیرد و فقط کشورهای خواسته‌شده را نگه می‌دارد.
func trimGeoIP(data []byte, keep []string) ([]byte, error) {
	var list router.GeoIPList
	if err := proto.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("unmarshal geoip: %w", err)
	}
	want := map[string]bool{}
	for _, k := range keep {
		want[strings.ToUpper(k)] = true
	}
	var kept []*router.GeoIP
	for _, e := range list.Entry {
		if want[strings.ToUpper(e.CountryCode)] {
			kept = append(kept, e)
		}
	}
	list.Entry = kept
	return proto.Marshal(&list)
}

// loadSource فایل را از مسیرِ env (در صورت ست‌بودن) می‌خواند، وگرنه از url دانلود می‌کند.
func loadSource(envKey, url string) ([]byte, error) {
	if p := os.Getenv(envKey); p != "" {
		fmt.Printf("  (using %s=%s)\n", envKey, p)
		return os.ReadFile(p)
	}
	fmt.Printf("  (downloading %s)\n", url)
	return download(url)
}

func download(url string) ([]byte, error) {
	c := &http.Client{Timeout: 120 * time.Second}
	resp, err := c.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: HTTP %d", url, resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "gengeo:", err)
	os.Exit(1)
}
