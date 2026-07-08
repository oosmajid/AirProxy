# VPN Bypass / Split-tunneling Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let the user route chosen destinations (Iran geo, custom domains, custom IPs) **directly**, bypassing the active proxy, via a new Xray `routing` section with bundled Iran-focused geo data.

**Architecture:** A pure `buildRouting(bypassRules)` translates persisted toggles/lists into an Xray `routing` map that is threaded through `buildConfig`/`Engine.Start`; matched destinations → `direct`, everything else → `proxy`. Iran-focused `geoip.dat` (trimmed to `ir`+`private`) and `geosite.dat` (community `geosite-lite`, category `CATEGORY-IR`) are `go:embed`-ded and extracted to the app-support dir at startup, with `XRAY_LOCATION_ASSET` pointed at them.

**Tech Stack:** Go 1.22+, Fyne v2, `github.com/xtls/xray-core@v1.260327.0`, `google.golang.org/protobuf/proto`.

## Global Constraints

- Xray version pinned: `github.com/xtls/xray-core v1.260327.0`.
- Outbound tags already present and reused: `proxy`, `direct`, `block`. Do **not** add/rename outbounds.
- Geo data is **bundled** (`go:embed`), extracted to `~/Library/Application Support/AirProxy/geo/`; env var to set is `XRAY_LOCATION_ASSET` (xray also accepts `xray.location.asset`).
- Bundled files: `assets/geo/geosite.dat` = community `geosite-lite.dat` (category `CATEGORY-IR`); `assets/geo/geoip.dat` = standard geoip **trimmed to `IR` + `PRIVATE`** only.
- Routing categories used: `geosite:category-ir`, `regexp:\.ir$`, `geoip:ir`, `geoip:private`; QUIC block = `network:"udp", port:"443"` → `block`.
- `domainStrategy`: `"AsIs"` (no local DNS).
- Default seed (new install and first upgrade): `Enabled=Iran=Private=BlockQUIC=true`, custom lists empty. Never wipe existing persisted `sources`/`configs`.
- Disabled/empty bypass ⇒ `buildRouting` returns `nil` ⇒ config is byte-equivalent to today.
- CLI mode passes `nil` routing (behavior unchanged).
- New `.go` files use Persian comments to match the existing codebase style.
- Work happens on branch `feature/vpn-bypass` (already created). Commit after every task.

---

### Task 1: Persistence — `bypassRules` model + default seed

**Files:**
- Modify: `persist.go` (add `bypassRules`, `defaultBypass`, `store.Bypass`; refactor `loadStore` to call a testable `decodeStore`)
- Test: `persist_test.go` (create)

**Interfaces:**
- Produces:
  - `type bypassRules struct { Enabled, Iran, Private, BlockQUIC bool; Domains, IPs []string }` (JSON tags `enabled`,`iran`,`private`,`block_quic`,`domains`,`ips`)
  - `func defaultBypass() bypassRules`
  - `func decodeStore(raw string) store`
  - `store` gains field `Bypass bypassRules \`json:"bypass"\``

- [ ] **Step 1: Write the failing test**

Create `persist_test.go`:

```go
package main

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestDecodeStoreSeedsBypassOnEmpty(t *testing.T) {
	s := decodeStore("")
	if !reflect.DeepEqual(s.Bypass, defaultBypass()) {
		t.Fatalf("empty store should seed default bypass, got %+v", s.Bypass)
	}
	if s.Listen != "127.0.0.1" || s.Socks != 10808 {
		t.Fatalf("empty store defaults wrong: %+v", s)
	}
}

func TestDecodeStoreSeedsBypassOnLegacyData(t *testing.T) {
	// دیتای نسخهٔ قبلی که اصلاً فیلد bypass ندارد.
	legacy := `{"sources":["s1"],"listen":"127.0.0.1","socks":10808,"http":10809,"rotate":true}`
	s := decodeStore(legacy)
	if !reflect.DeepEqual(s.Bypass, defaultBypass()) {
		t.Fatalf("legacy data should seed default bypass, got %+v", s.Bypass)
	}
	if len(s.Sources) != 1 || s.Sources[0] != "s1" {
		t.Fatalf("legacy sources must be preserved, got %+v", s.Sources)
	}
}

func TestDecodeStoreRespectsSavedBypass(t *testing.T) {
	// کاربری که همه‌چیز را خاموش کرده و ذخیره کرده است.
	saved := store{Listen: "127.0.0.1", Socks: 10808, Bypass: bypassRules{Enabled: false}}
	b, _ := json.Marshal(saved)
	got := decodeStore(string(b))
	if got.Bypass.Enabled || got.Bypass.Iran {
		t.Fatalf("saved all-off bypass must be respected, got %+v", got.Bypass)
	}
}

func TestBypassJSONRoundTrip(t *testing.T) {
	in := bypassRules{Enabled: true, Iran: true, Private: false, BlockQUIC: true,
		Domains: []string{"a.ir"}, IPs: []string{"10.0.0.0/8"}}
	b, _ := json.Marshal(in)
	var out bypassRules
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Fatalf("round-trip mismatch: %+v vs %+v", in, out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test . -run 'DecodeStore|Bypass' -v`
Expected: FAIL — `undefined: decodeStore`, `undefined: defaultBypass`, `bypassRules` unknown.

- [ ] **Step 3: Write minimal implementation**

In `persist.go`, add after the `cfgItem` type (before `store`):

```go
// bypassRules قوانین دور زدن پروکسی (split-tunneling) را نگه می‌دارد.
// مقصدهای مطابق این قوانین مستقیم (direct) می‌روند، نه از داخل پروکسی.
type bypassRules struct {
	Enabled   bool     `json:"enabled"`    // کلید اصلی روشن/خاموش
	Iran      bool     `json:"iran"`       // geosite:category-ir + ‎regexp:\.ir$‎ + geoip:ir
	Private   bool     `json:"private"`    // geoip:private (شبکهٔ محلی)
	BlockQUIC bool     `json:"block_quic"` // بلاک udp/443 تا routing مبتنی بر دامنه درست بماند
	Domains   []string `json:"domains"`    // دامنه‌های دلخواه → direct
	IPs       []string `json:"ips"`        // IP/CIDR های دلخواه → direct
}

// defaultBypass مقدار پیش‌فرض را برمی‌گرداند: bypass روشن با ایران/محلی/بلاک-QUIC.
func defaultBypass() bypassRules {
	return bypassRules{Enabled: true, Iran: true, Private: true, BlockQUIC: true}
}
```

Add the field to `store`:

```go
type store struct {
	Sources []string    `json:"sources"`
	Configs []cfgItem   `json:"configs"`
	Listen  string      `json:"listen"`
	Socks   int         `json:"socks"`
	HTTP    int         `json:"http"`
	Rotate  bool        `json:"rotate"`
	Bypass  bypassRules `json:"bypass"`
}
```

Replace the existing `loadStore` body with a thin wrapper over a pure `decodeStore`:

```go
func loadStore(p fyne.Preferences) store {
	return decodeStore(p.String(prefKey))
}

// decodeStore رشتهٔ ذخیره‌شده را به store تبدیل می‌کند و مقادیر پیش‌فرض را ست می‌کند.
// چون Bypass پیش از Unmarshal روی defaultBypass() ست می‌شود، دیتای قدیمیِ فاقد
// فیلد bypass به‌صورت خودکار «ایران روشن» می‌شود، ولی اگر کاربر عمداً خاموشش کرده
// باشد همان مقدار ذخیره‌شده اعمال می‌گردد.
func decodeStore(raw string) store {
	s := store{Listen: "127.0.0.1", Socks: 10808, HTTP: 10809, Rotate: true, Bypass: defaultBypass()}
	if raw == "" {
		return s
	}
	_ = json.Unmarshal([]byte(raw), &s)
	if s.Listen == "" {
		s.Listen = "127.0.0.1"
	}
	if s.Socks == 0 {
		s.Socks = 10808
	}
	return s
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test . -run 'DecodeStore|Bypass' -v`
Expected: PASS (4 tests).

- [ ] **Step 5: Commit**

```bash
git add persist.go persist_test.go
git commit -m "feat(persist): add bypassRules model with default-on Iran seed"
```

---

### Task 2: Routing translation — `parseList` + `buildRouting`

**Files:**
- Create: `routing.go`
- Test: `routing_test.go`

**Interfaces:**
- Consumes: `bypassRules` (Task 1)
- Produces:
  - `func parseList(text string) []string` — splits lines, trims, drops blanks and `#` comments
  - `func buildRouting(b bypassRules) map[string]interface{}` — Xray `routing` map, or `nil` when disabled / no rules

- [ ] **Step 1: Write the failing test**

Create `routing_test.go`:

```go
package main

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestParseList(t *testing.T) {
	got := parseList("  a.com \n\n# a comment\nb.ir\n\t\ndomain:x.ir\n")
	want := []string{"a.com", "b.ir", "domain:x.ir"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
}

func TestBuildRoutingDisabledIsNil(t *testing.T) {
	if buildRouting(bypassRules{Enabled: false, Iran: true}) != nil {
		t.Fatal("disabled bypass must produce nil routing")
	}
}

func TestBuildRoutingEnabledButEmptyIsNil(t *testing.T) {
	if buildRouting(bypassRules{Enabled: true}) != nil {
		t.Fatal("enabled bypass with no active rule must produce nil routing")
	}
}

func TestBuildRoutingFullPresets(t *testing.T) {
	r := buildRouting(bypassRules{Enabled: true, Iran: true, Private: true, BlockQUIC: true})
	got, _ := json.Marshal(r)
	want := `{"domainStrategy":"AsIs","rules":[` +
		`{"network":"udp","outboundTag":"block","port":"443","type":"field"},` +
		`{"domain":["geosite:category-ir","regexp:\\.ir$"],"outboundTag":"direct","type":"field"},` +
		`{"ip":["geoip:ir"],"outboundTag":"direct","type":"field"},` +
		`{"ip":["geoip:private"],"outboundTag":"direct","type":"field"}]}`
	if string(got) != want {
		t.Fatalf("\n got: %s\nwant: %s", got, want)
	}
}

func TestBuildRoutingCustomLists(t *testing.T) {
	r := buildRouting(bypassRules{Enabled: true, Domains: []string{"a.com"}, IPs: []string{"1.2.3.0/24"}})
	got, _ := json.Marshal(r)
	want := `{"domainStrategy":"AsIs","rules":[` +
		`{"domain":["a.com"],"outboundTag":"direct","type":"field"},` +
		`{"ip":["1.2.3.0/24"],"outboundTag":"direct","type":"field"}]}`
	if string(got) != want {
		t.Fatalf("\n got: %s\nwant: %s", got, want)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test . -run 'ParseList|BuildRouting' -v`
Expected: FAIL — `undefined: parseList`, `undefined: buildRouting`.

- [ ] **Step 3: Write minimal implementation**

Create `routing.go`:

```go
package main

import "strings"

// parseList متن چندخطی را به فهرست ورودی‌های تروشده تبدیل می‌کند؛
// خطوط خالی و خطوط کامنت (#) نادیده گرفته می‌شوند.
func parseList(text string) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	return out
}

// buildRouting بخش routing کانفیگ Xray را از روی قوانین bypass می‌سازد.
// اگر bypass خاموش باشد یا هیچ قانونی تولید نشود nil برمی‌گرداند؛ یعنی رفتار
// دقیقاً مثل قبل می‌ماند (همهٔ ترافیک از پروکسی می‌رود). مقصدهای مطابق قوانین
// به اوت‌باند direct می‌روند.
func buildRouting(b bypassRules) map[string]interface{} {
	if !b.Enabled {
		return nil
	}
	var rules []map[string]interface{}

	// بلاک QUIC (udp/443) تا sniffing و routing مبتنی بر دامنه درست کار کند.
	if b.BlockQUIC {
		rules = append(rules, map[string]interface{}{
			"type": "field", "outboundTag": "block", "network": "udp", "port": "443",
		})
	}
	// دامنه‌های دلخواه → direct
	if len(b.Domains) > 0 {
		rules = append(rules, map[string]interface{}{
			"type": "field", "outboundTag": "direct", "domain": b.Domains,
		})
	}
	// IP/CIDR های دلخواه → direct
	if len(b.IPs) > 0 {
		rules = append(rules, map[string]interface{}{
			"type": "field", "outboundTag": "direct", "ip": b.IPs,
		})
	}
	// ایران: دامنه‌ها (geosite + همهٔ ‎.ir‎) و IP ها → direct
	if b.Iran {
		rules = append(rules, map[string]interface{}{
			"type": "field", "outboundTag": "direct",
			"domain": []string{"geosite:category-ir", "regexp:\\.ir$"},
		})
		rules = append(rules, map[string]interface{}{
			"type": "field", "outboundTag": "direct", "ip": []string{"geoip:ir"},
		})
	}
	// شبکهٔ محلی/خصوصی → direct
	if b.Private {
		rules = append(rules, map[string]interface{}{
			"type": "field", "outboundTag": "direct", "ip": []string{"geoip:private"},
		})
	}

	if len(rules) == 0 {
		return nil
	}
	return map[string]interface{}{
		"domainStrategy": "AsIs",
		"rules":          rules,
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test . -run 'ParseList|BuildRouting' -v`
Expected: PASS (5 tests).

- [ ] **Step 5: Commit**

```bash
git add routing.go routing_test.go
git commit -m "feat(routing): translate bypassRules into Xray routing rules"
```

---

### Task 3: Config plumbing — thread `routing` through `buildConfig` and `Engine.Start`

Signature-only change; every call site passes `nil` for now, so runtime behavior is unchanged.

**Files:**
- Modify: `config.go` (`buildConfig` gains a `routing` param)
- Modify: `engine.go` (`Engine.Start` gains a `routing` param, passes it down)
- Modify: `main.go:63` (`runCLI` passes `nil`)
- Modify: `gui.go:536` and `gui.go:574` (both `eng.Start(...)` calls pass `nil`)
- Test: `config_test.go` (create)

**Interfaces:**
- Consumes: nothing new
- Produces:
  - `func buildConfig(listen string, socksPort, httpPort int, outbound map[string]interface{}, routing map[string]interface{}) map[string]interface{}`
  - `func (e *Engine) Start(link, listen string, socksPort, httpPort int, routing map[string]interface{}) error`

- [ ] **Step 1: Write the failing test**

Create `config_test.go`:

```go
package main

import "testing"

func TestBuildConfigOmitsRoutingWhenNil(t *testing.T) {
	cfg := buildConfig("127.0.0.1", 10808, 0, map[string]interface{}{"tag": "proxy"}, nil)
	if _, ok := cfg["routing"]; ok {
		t.Fatal("nil routing must not add a routing section")
	}
}

func TestBuildConfigIncludesRoutingWhenSet(t *testing.T) {
	routing := map[string]interface{}{"domainStrategy": "AsIs"}
	cfg := buildConfig("127.0.0.1", 10808, 0, map[string]interface{}{"tag": "proxy"}, routing)
	got, ok := cfg["routing"].(map[string]interface{})
	if !ok || got["domainStrategy"] != "AsIs" {
		t.Fatalf("routing not wired through, got %v", cfg["routing"])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test . -run 'BuildConfig' -v`
Expected: FAIL to compile — `buildConfig` called with 5 args but defined with 4.

- [ ] **Step 3: Write minimal implementation**

In `config.go`, change the signature and append routing. Replace the `return` block:

```go
func buildConfig(listen string, socksPort, httpPort int, outbound map[string]interface{}, routing map[string]interface{}) map[string]interface{} {
	// ... inbounds block unchanged ...

	cfg := map[string]interface{}{
		"log": map[string]interface{}{
			"loglevel": "warning",
		},
		"inbounds": inbounds,
		"outbounds": []map[string]interface{}{
			outbound,
			{"tag": "direct", "protocol": "freedom"},
			{"tag": "block", "protocol": "blackhole"},
		},
	}
	if routing != nil {
		cfg["routing"] = routing
	}
	return cfg
}
```

In `engine.go`, change `Start` and its `buildConfig` call:

```go
func (e *Engine) Start(link, listen string, socksPort, httpPort int, routing map[string]interface{}) error {
	// ... unchanged body until buildConfig ...
	cfg := buildConfig(listen, socksPort, httpPort, outbound, routing)
	// ... rest unchanged ...
}
```

In `main.go` (the `runCLI` call, currently `eng.Start(chosen, listen, socks, httpP)`):

```go
	if err := eng.Start(chosen, listen, socks, httpP, nil); err != nil {
```

In `gui.go`, the rotation call inside `startMonitor` (currently `eng.Start(nr, listen, socks, httpP)`):

```go
					if err := eng.Start(nr, listen, socks, httpP, nil); err != nil {
```

In `gui.go`, the `doConnect` call (currently `eng.Start(raw, listen, socks, httpP)`):

```go
		if err := eng.Start(raw, listen, socks, httpP, nil); err != nil {
```

- [ ] **Step 4: Run test to verify it passes and everything builds**

Run: `go build ./... && go test . -run 'BuildConfig|DecodeStore|Bypass|ParseList|BuildRouting' -v`
Expected: build succeeds; all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add config.go config_test.go engine.go main.go gui.go
git commit -m "refactor(engine): thread optional routing through buildConfig/Start (nil for now)"
```

---

### Task 4: Geo assets — generator, bundled `.dat` files, runtime extraction

**Files:**
- Create: `tools/gengeo/main.go` (maintainer tool: download + trim)
- Create: `tools/gengeo/main_test.go` (unit test for `trimGeoIP`)
- Create: `assets/geo/geoip.dat`, `assets/geo/geosite.dat` (generated, committed)
- Create: `geo.go` (`//go:embed` + extraction)
- Create: `geo_test.go` (extraction + xray load smoke test)
- Modify: `go.mod` / `go.sum` (promote `google.golang.org/protobuf` to direct via `go mod tidy`)

**Interfaces:**
- Consumes: `buildConfig` (Task 3), `buildRouting` (Task 2)
- Produces:
  - `tools/gengeo`: `func trimGeoIP(data []byte, keep []string) ([]byte, error)`
  - `geo.go`: `func ensureGeoAssets() (string, error)`, `func extractGeoTo(dir string) error`, `var geoipDat, geositeDat []byte`

- [ ] **Step 1: Write the failing test for the trimmer**

Create `tools/gengeo/main_test.go`:

```go
package main

import (
	"testing"

	"github.com/xtls/xray-core/app/router"
	"google.golang.org/protobuf/proto"
)

func TestTrimGeoIPKeepsOnlyWanted(t *testing.T) {
	in := &router.GeoIPList{Entry: []*router.GeoIP{
		{CountryCode: "IR", Cidr: []*router.CIDR{{Ip: []byte{1, 2, 3, 0}, Prefix: 24}}},
		{CountryCode: "US", Cidr: []*router.CIDR{{Ip: []byte{5, 6, 7, 0}, Prefix: 24}}},
		{CountryCode: "PRIVATE", Cidr: []*router.CIDR{{Ip: []byte{10, 0, 0, 0}, Prefix: 8}}},
	}}
	data, err := proto.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	out, err := trimGeoIP(data, []string{"IR", "PRIVATE"})
	if err != nil {
		t.Fatal(err)
	}
	var got router.GeoIPList
	if err := proto.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Entry) != 2 {
		t.Fatalf("want 2 kept entries, got %d", len(got.Entry))
	}
	for _, e := range got.Entry {
		if e.CountryCode == "US" {
			t.Fatal("US should have been trimmed")
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./tools/gengeo/ -v`
Expected: FAIL — `undefined: trimGeoIP`.

- [ ] **Step 3: Write the generator**

Create `tools/gengeo/main.go`:

```go
package main

// gengeo فایل‌های geo سبک و مخصوص ایران را می‌سازد و در assets/geo/ می‌نویسد:
//   - geosite.dat : همان geosite-lite جامعه (فقط دستهٔ CATEGORY-IR)
//   - geoip.dat   : geoip استاندارد که فقط به IR + PRIVATE تریم شده است
// این ابزار را دستی اجرا کنید تا دیتای geo به‌روزرسانی شود:
//   go run ./tools/gengeo assets/geo
// خروجی در ریپو commit می‌شود؛ کاربر نهایی نیازی به اجرای آن ندارد.

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

	fmt.Println("downloading geosite-lite …")
	site, err := download(geositeURL)
	if err != nil {
		fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "geosite.dat"), site, 0o644); err != nil {
		fatal(err)
	}
	fmt.Printf("  geosite.dat: %d bytes\n", len(site))

	fmt.Println("downloading geoip …")
	ipFull, err := download(geoipURL)
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
```

- [ ] **Step 4: Ensure the protobuf dep is direct, then run the trimmer test**

Run: `go mod tidy && go test ./tools/gengeo/ -v`
Expected: `go.mod` now lists `google.golang.org/protobuf` as a direct require; test PASSES.

- [ ] **Step 5: Generate the committed geo assets**

Run: `go run ./tools/gengeo assets/geo`
Expected output (sizes approximate):
```
downloading geosite-lite …
  geosite.dat: ~2050000 bytes
downloading geoip …
  geoip.dat: ~200000-500000 bytes (trimmed from ~17000000)
```
Verify: `ls -lh assets/geo/` shows both files; `strings assets/geo/geosite.dat | grep -c CATEGORY-IR` ≥ 1.

> If the network is unavailable in the execution environment, source the full geoip from the local file instead — temporarily add an `os.Getenv("GEOIP_SRC")` fallback, or run once with connectivity. The committed output is what matters.

- [ ] **Step 6: Write the failing test for runtime extraction + xray load**

Create `geo_test.go`:

```go
package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/xtls/xray-core/infra/conf/serial"
)

func TestExtractGeoToWritesFiles(t *testing.T) {
	dir := t.TempDir()
	if err := extractGeoTo(dir); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"geoip.dat", "geosite.dat"} {
		fi, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("%s missing: %v", name, err)
		}
		if fi.Size() == 0 {
			t.Fatalf("%s is empty", name)
		}
	}
}

// اثبات اینکه فایل‌های bundle‌شده واقعاً برای Xray قابل بارگذاری‌اند و شامل
// دسته‌های geoip:ir / geosite:category-ir / geoip:private هستند.
func TestBundledGeoLoadsInXray(t *testing.T) {
	dir := t.TempDir()
	if err := extractGeoTo(dir); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XRAY_LOCATION_ASSET", dir)

	cfg := buildConfig("127.0.0.1", 0, 0,
		map[string]interface{}{"tag": "proxy", "protocol": "freedom"},
		buildRouting(bypassRules{Enabled: true, Iran: true, Private: true, BlockQUIC: true}),
	)
	jsonBytes, _ := json.Marshal(cfg)
	if _, err := serial.LoadJSONConfig(bytes.NewReader(jsonBytes)); err != nil {
		t.Fatalf("xray failed to load bundled geo (geoip:ir / geosite:category-ir): %v", err)
	}
}
```

- [ ] **Step 7: Run to verify it fails**

Run: `go test . -run 'Geo' -v`
Expected: FAIL — `undefined: extractGeoTo` (and the `//go:embed` vars don't exist yet).

- [ ] **Step 8: Write `geo.go`**

Create `geo.go`:

```go
package main

import (
	_ "embed"
	"os"
	"path/filepath"
)

//go:embed assets/geo/geoip.dat
var geoipDat []byte

//go:embed assets/geo/geosite.dat
var geositeDat []byte

// ensureGeoAssets فایل‌های geo را در پوشهٔ پشتیبان اپ می‌نویسد (اگر نبود/تغییر
// اندازه داشت) و XRAY_LOCATION_ASSET را به آن پوشه اشاره می‌دهد. مسیر را برمی‌گرداند.
func ensureGeoAssets() (string, error) {
	dir, err := geoDir()
	if err != nil {
		return "", err
	}
	if err := extractGeoTo(dir); err != nil {
		return "", err
	}
	// Xray هم "xray.location.asset" و هم فرم نرمال‌شدهٔ XRAY_LOCATION_ASSET را می‌خواند.
	os.Setenv("XRAY_LOCATION_ASSET", dir)
	return dir, nil
}

func geoDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "Application Support", "AirProxy", "geo"), nil
}

// extractGeoTo هر دو فایل embed‌شده را در dir می‌نویسد (idempotent).
func extractGeoTo(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := writeIfChanged(filepath.Join(dir, "geoip.dat"), geoipDat); err != nil {
		return err
	}
	return writeIfChanged(filepath.Join(dir, "geosite.dat"), geositeDat)
}

// writeIfChanged فایل را فقط وقتی می‌نویسد که موجود نباشد یا اندازه‌اش فرق کند.
func writeIfChanged(path string, data []byte) error {
	if fi, err := os.Stat(path); err == nil && fi.Size() == int64(len(data)) {
		return nil
	}
	return os.WriteFile(path, data, 0o644)
}
```

- [ ] **Step 9: Run tests to verify they pass**

Run: `go test . -run 'Geo' -v`
Expected: PASS — both `TestExtractGeoToWritesFiles` and `TestBundledGeoLoadsInXray`. (The second proves the trimmed/lite files parse in Xray and contain the categories.)

- [ ] **Step 10: Commit**

```bash
git add tools/gengeo/main.go tools/gengeo/main_test.go assets/geo/geoip.dat assets/geo/geosite.dat geo.go geo_test.go go.mod go.sum
git commit -m "feat(geo): bundle Iran-focused geoip/geosite and extract at runtime"
```

---

### Task 5: Wire bypass into the GUI (activate the feature + docs)

Extract geo at startup, track live `bypass` state, pass real routing into connect/rotate, add the Bypass dialog, and document it.

**Files:**
- Modify: `gui.go` (startup extraction; `bypass` var + `currentBypass`; `persist` includes Bypass; both `eng.Start` calls use `buildRouting(currentBypass())`; `showBypassDialog`; button in Settings)
- Modify: `README.md` (document the feature + `gengeo`)

**Interfaces:**
- Consumes: `ensureGeoAssets` (Task 4), `buildRouting`/`parseList` (Task 2), `bypassRules`/`defaultBypass` (Task 1), `store.Bypass` (Task 1)

- [ ] **Step 1: Extract geo assets at startup**

In `gui.go` `runGUI`, immediately after `st := loadStore(prefs)`:

```go
	if _, err := ensureGeoAssets(); err != nil {
		fmt.Println("geo assets:", err) // بدون geo، bypass ژئویی کار نمی‌کند؛ ادامه بده.
	}
```

- [ ] **Step 2: Track live bypass state**

In the `var ( … )` block (with `sources`, `configs`, …), add:

```go
		bypass = st.Bypass
```

After the `allRaws`/`nameOf` helpers (near the connect section), add a locked accessor:

```go
	currentBypass := func() bypassRules {
		mu.Lock()
		defer mu.Unlock()
		return bypass
	}
```

- [ ] **Step 3: Persist bypass**

In the `persist` closure, add `Bypass` to the constructed `store` (read under the existing lock — move the read inside the `mu.Lock()` region):

```go
	persist := func() {
		l, s, h, _ := readSettings()
		mu.Lock()
		stt := store{
			Sources: append([]string{}, sources...),
			Configs: append([]cfgItem{}, configs...),
			Listen:  l, Socks: s, HTTP: h, Rotate: rotateCheck.Checked,
			Bypass: bypass,
		}
		mu.Unlock()
		saveStore(prefs, stt)
	}
```

- [ ] **Step 4: Pass real routing into connect + rotate**

Replace the two `eng.Start(..., nil)` calls added in Task 3 with live routing.

In `doConnect`:

```go
		if err := eng.Start(raw, listen, socks, httpP, buildRouting(currentBypass())); err != nil {
```

In `startMonitor` rotation:

```go
					if err := eng.Start(nr, listen, socks, httpP, buildRouting(currentBypass())); err != nil {
```

- [ ] **Step 5: Add the Bypass dialog**

In `gui.go`, just before `gearBtn` is defined, add `showBypassDialog` (builds fresh widgets from current `bypass` each open, so Cancel is non-destructive):

```go
	showBypassDialog := func() {
		cur := currentBypass()
		enable := widget.NewCheck("Enable bypass (split-tunneling)", nil)
		enable.SetChecked(cur.Enabled)
		iran := widget.NewCheck("Iran — Iranian sites & IPs (geo)", nil)
		iran.SetChecked(cur.Iran)
		priv := widget.NewCheck("Private / LAN networks", nil)
		priv.SetChecked(cur.Private)
		quic := widget.NewCheck("Block QUIC (UDP/443) for correct routing", nil)
		quic.SetChecked(cur.BlockQUIC)

		domains := widget.NewMultiLineEntry()
		domains.Wrapping = fyne.TextWrapBreak
		domains.SetText(strings.Join(cur.Domains, "\n"))
		domains.SetPlaceHolder("Custom domains, one per line\n(e.g. digikala.com  ·  domain:example.ir  ·  regexp:\\.ir$)")
		ips := widget.NewMultiLineEntry()
		ips.Wrapping = fyne.TextWrapBreak
		ips.SetText(strings.Join(cur.IPs, "\n"))
		ips.SetPlaceHolder("Custom IPs / CIDRs, one per line\n(e.g. 5.160.0.0/16  ·  geoip:ir)")

		form := container.NewVBox(
			enable,
			widget.NewSeparator(),
			iran, priv, quic,
			layoutSpacer(4),
			labeled("Bypass domains (direct)", container.New(&fixedHeight{80}, domains)),
			labeled("Bypass IPs / CIDRs (direct)", container.New(&fixedHeight{80}, ips)),
		)
		d := dialog.NewCustomConfirm("Bypass / split-tunneling", "Save", "Cancel",
			container.New(&fixedHeight{440}, container.NewVScroll(form)), func(ok bool) {
				if !ok {
					return
				}
				mu.Lock()
				bypass = bypassRules{
					Enabled:   enable.Checked,
					Iran:      iran.Checked,
					Private:   priv.Checked,
					BlockQUIC: quic.Checked,
					Domains:   parseList(domains.Text),
					IPs:       parseList(ips.Text),
				}
				conn := connectedRaw
				mu.Unlock()
				persist()
				// اگر متصل هستیم، با routing جدید دوباره وصل شو تا فوراً اعمال شود.
				if eng.Running() && conn != "" {
					doConnect(conn)
				}
			}, w)
		d.Resize(fyne.NewSize(400, 560))
		d.Show()
	}
```

- [ ] **Step 6: Add a button to the Settings dialog**

Inside `gearBtn`'s `OnTapped`, add a bypass button and include it in the form. Replace the form construction:

```go
	gearBtn := widget.NewButtonWithIcon("", theme.SettingsIcon(), func() {
		bypassBtn := widget.NewButtonWithIcon("Bypass / split-tunneling…", theme.MailForwardIcon(), showBypassDialog)
		bypassBtn.Importance = widget.LowImportance
		form := container.NewVBox(
			labeled("IP", listenEntry),
			labeled("SOCKS5 port", socksEntry),
			labeled("HTTP port (optional)", httpEntry),
			layoutSpacer(4),
			rotateCheck,
			layoutSpacer(8),
			bypassBtn,
			layoutSpacer(4),
		)
		d := dialog.NewCustomConfirm("Settings", "Save", "Close", form, func(bool) { persist() }, w)
		d.Resize(fyne.NewSize(380, 380))
		d.Show()
	})
	gearBtn.Importance = widget.LowImportance
```

- [ ] **Step 7: Build and run the whole test suite**

Run: `go build ./... && go test ./... -v`
Expected: build OK; all tests PASS (persist, routing, config, geo, gengeo).

- [ ] **Step 8: Update README**

In `README.md`, add a row to the Features table:

```
| 🔀 **Bypass / split-tunneling** | Route Iran (geo), or your own domains/IPs, **directly** instead of through the proxy. Iran bypass is on by default. |
```

And add a short section after the SSH tunnel section:

```markdown
### 🔀 Bypass (split-tunneling)

Open **⚙️ Settings → Bypass / split-tunneling…**. Toggle whole rule-sets — **Iran** (Iranian sites & IPs via bundled geo data), **Private / LAN**, **Block QUIC** — and/or add your own **domains** and **IPs/CIDRs** (one per line; `domain:`, `regexp:`, `geoip:` prefixes accepted). Matched destinations go **direct** (bypassing the proxy); everything else stays proxied. Iran bypass is enabled by default.

> Geo data (`geoip.dat` / `geosite.dat`, Iran-focused) is bundled in the app. Maintainers can refresh it with `go run ./tools/gengeo assets/geo`.
```

- [ ] **Step 9: Commit**

```bash
git add gui.go README.md
git commit -m "feat(gui): bypass/split-tunneling dialog wired into connect + rotation"
```

---

## Final Verification (manual, no commit)

REQUIRED SUB-SKILL for this section: `superpowers:verification-before-completion`.

- [ ] **Build the app bundle and launch it**

Run: `go build -o airproxy . && ./airproxy` (or the normal app-bundle build). Confirm it starts with no geo errors on stdout.

- [ ] **Confirm geo extraction**

Run: `ls -lh ~/Library/Application\ Support/AirProxy/geo/` — both `geoip.dat` and `geosite.dat` present.

- [ ] **Add any working server, connect, and test routing** (Iran bypass is on by default):

```bash
# Foreign site → should return the PROXY server's IP:
curl --socks5-hostname 127.0.0.1:10808 https://api.ipify.org ; echo
# Iranian site → should connect directly (real IP / reachable), e.g.:
curl --socks5-hostname 127.0.0.1:10808 -s -o /dev/null -w '%{http_code}\n' https://www.digikala.com
```
Expected: foreign IP = proxy exit IP; the `.ir`/Iranian request succeeds via the direct path.

- [ ] **Toggle test:** open the Bypass dialog, uncheck **Iran**, Save → it reconnects. Now the Iranian site also routes through the proxy (foreign IP). Re-check Iran → back to direct.

- [ ] **Persistence test:** quit and relaunch → bypass settings are retained.

---

## Self-Review notes (author)

- **Spec coverage:** geo bundling (T4), routing translation (T2), custom domain/IP lists (T2 + T5 dialog), Iran default-on seed (T1), presets+custom UX (T5), config wiring (T3), CLI unchanged/nil (T3), README (T5), tests (all tasks). ✓
- **Type consistency:** `bypassRules`, `buildRouting`, `parseList`, `ensureGeoAssets`/`extractGeoTo`, `trimGeoIP`, `buildConfig`(5-arg), `Engine.Start`(5-arg), `currentBypass` used identically across tasks. ✓
- **No placeholders:** every code step contains complete code; commands have expected output. ✓
