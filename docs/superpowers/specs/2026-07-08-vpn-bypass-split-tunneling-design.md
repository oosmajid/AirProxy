# AirProxy — VPN Bypass / Split-tunneling — Design

**Date:** 2026-07-08
**Status:** Design approved by user (presets + custom fields, bundled geo data); pending spec review.

## 1. Goal

Let the user route selected destinations **directly** — bypassing the active proxy/VPN — instead of tunneling them. Three kinds of bypass targets, matching the request:

1. **Geo of a country** — primarily Iran (`geoip:ir` + `geosite:category-ir`).
2. **Custom IPs / CIDRs.**
3. **Custom domains.**

This mirrors the standard v2rayN "bypass Iran" routing the user already runs. Ship with Iran bypass enabled by default, and support several toggleable ("diverse") lists.

Reference — the user's current v2rayN generated routing (verified on disk), which we reproduce:

```
block   udp/443            (QUIC — so TLS sniffing/routing stays correct)
direct  ip geoip:private
direct  domain geosite:private
direct  domain geosite:category-ir
direct  ip geoip:ir
proxy   (everything else, default)
```

We reproduce this, with one deliberate change: `geosite:private` is dropped because the bundled `geosite-lite.dat` contains only `CATEGORY-IR`; local/LAN destinations are already covered by the `geoip:private` IP rule, so the domain-side private rule is redundant for our purposes.

The outbound tags v2rayN uses (`proxy` / `direct` / `block`) are **already present** in AirProxy's `buildConfig`, so no outbound changes are needed — only a new `routing` section.

## 2. Non-goals (YAGNI)

- No full named multi-list manager. Chosen UX: **presets + two custom fields**.
- No per-server / per-config routing. Routing is a single global policy.
- No ad/malware **block** lists. Only bypass→`direct`. (Blocking QUIC is the one exception, purely for routing correctness.)
- No in-app online geo auto-update UI. Geo data is **bundled**; refreshing it is a maintainer tool (`tools/gengeo`).
- CLI mode (`--link` / `--sub`) keeps today's behavior (no routing) for now — passes `nil` routing.

## 3. User-facing behavior

When bypass is enabled, the generated Xray config gains a `routing` section. Matched destinations → `direct`; everything unmatched → `proxy` (Xray's default = first outbound). When disabled, config is byte-for-byte equivalent to today.

Global, persisted settings:

| Control | Effect |
|---|---|
| **Enable bypass** (master) | Off → no `routing` section at all (identical to current app). |
| **Iran — sites & IPs** | `direct` for `domain: ["geosite:category-ir", "regexp:\\.ir$"]` and `ip: ["geoip:ir"]`. |
| **Private / LAN** | `direct` for `ip: ["geoip:private"]`. |
| **Block QUIC (UDP/443)** | `block` for `network: udp, port: 443`. Keeps sniff-based routing correct. |
| **Custom domains** | multiline, one per line → `direct`. |
| **Custom IPs / CIDRs** | multiline, one per line → `direct`. |

**Default seed** (new install *and* first upgrade): Enable=on, Iran=on, Private=on, BlockQUIC=on, custom fields empty. The bundled `geosite:category-ir` **is** the "Iranian sites" list the user asked to be pre-populated.

## 4. Architecture

### 4.1 Geo data bundle
Two files are committed to the repo and embedded into the binary:

- `assets/geo/geosite.dat` — the community **`geosite-lite.dat`** (~2 MB; contains exactly one category, `CATEGORY-IR`). Used as-is.
- `assets/geo/geoip.dat` — the standard geoip trimmed to **`ir` + `private`** only (~a few hundred KB instead of 17 MB).

`tools/gengeo/main.go` (new; same convention as the existing `tools/genicon` and `tools/genbanner` build-time asset generators):
- Downloads `geosite-lite.dat` and the full `geoip.dat` from the community release.
- Trims geoip: unmarshal `github.com/xtls/xray-core/app/router` `GeoIPList` (via `google.golang.org/protobuf/proto`), keep entries whose `CountryCode` ∈ {`IR`, `PRIVATE`}, re-marshal.
- Writes both files into `assets/geo/`. Run manually by a maintainer to refresh; output is committed, so end users/builders never need to run it (exactly like `icon.png`/`banner.png`).

`geo.go` (new):
- `//go:embed assets/geo/geoip.dat` and `assets/geo/geosite.dat` → two `[]byte`.
- `ensureGeoAssets() (string, error)`: writes the embedded files to `~/Library/Application Support/AirProxy/geo/` if missing or size-mismatched, then `os.Setenv("XRAY_LOCATION_ASSET", dir)` and returns the dir.
- Env var verified: xray reads `xray.location.asset` **or** its normalized `XRAY_LOCATION_ASSET` (`common/platform`), searching that dir for `geoip.dat`/`geosite.dat` before the executable dir.
- Called once at GUI startup, and defensively before any `Engine.Start` whose routing references geo.

### 4.2 Routing translation
`routing.go` (new):
- `buildRouting(b bypassRules) map[string]interface{}` → the Xray `routing` object, or **`nil`** when `!b.Enabled` or no active rule produces output.
- Rule assembly (block first, then direct rules; order among direct rules is immaterial):
  1. BlockQUIC → `{type:field, outboundTag:block, network:"udp", port:"443"}`
  2. Custom domains → `{type:field, outboundTag:direct, domain:[...]}`
  3. Custom IPs → `{type:field, outboundTag:direct, ip:[...]}`
  4. Iran → `{...,direct, domain:["geosite:category-ir","regexp:\\.ir$"]}` and `{...,direct, ip:["geoip:ir"]}`
  5. Private → `{...,direct, ip:["geoip:private"]}`
- `domainStrategy` omitted → defaults to `AsIs` (no local DNS; keeps it light). Existing inbound sniffing (`destOverride:["http","tls"]`) recovers domains so domain rules apply to TLS/HTTP connects. `IPIfNonMatch` is a possible future toggle.
- Helper `parseList(text string) []string`: split on newlines, trim, drop blank lines and `#` comments. Domains/IPs are passed through verbatim so power users can write `domain:`, `regexp:`, `geosite:`, `geoip:`, bare hosts, or CIDRs.

### 4.3 Config wiring
- `config.go`: `buildConfig(listen, socks, http, outbound, routing map[string]interface{})` — when `routing != nil`, add `"routing": routing` to the returned map.
- `engine.go`: `Engine.Start(link, listen, socks, http int, routing map[string]interface{})` — thread `routing` into `buildConfig`. Engine stays free of app-model types (it receives a prebuilt map, not `bypassRules`).
- Call sites:
  - `main.go` `runCLI` → `eng.Start(chosen, listen, socks, httpP, nil)`.
  - `gui.go` `doConnect` and `startMonitor` rotation → `ensureGeoAssets()` (if geo referenced) then `eng.Start(raw, listen, socks, httpP, buildRouting(currentBypass()))`.

### 4.4 Data model & persistence (`persist.go`)
```go
type bypassRules struct {
    Enabled   bool     `json:"enabled"`
    Iran      bool     `json:"iran"`
    Private   bool     `json:"private"`
    BlockQUIC bool     `json:"block_quic"`
    Domains   []string `json:"domains"`
    IPs       []string `json:"ips"`
}
```
- Add `Bypass bypassRules` to `store`.
- First-run / upgrade seed: after unmarshal, probe for absence of the key with a pointer struct
  (`struct{ Bypass *bypassRules \`json:"bypass"\` }`); if `nil`, assign `defaultBypass()` (Enabled/Iran/Private/BlockQUIC = true). This gives existing users the Iran-on default on upgrade without wiping their sources/configs.
- `saveStore` unchanged (whole `store` marshaled).

### 4.5 UI (`gui.go`)
- Entry point: a **"Bypass / split-tunneling…"** button inside the existing Settings dialog, opening a dedicated dialog (keeps the small 380×340 Settings box uncluttered; gives room for two textareas).
- Dialog widgets: master `Check`; Iran / Private / BlockQUIC `Check`s; two `MultiLineEntry`s (domains, IPs) with guiding placeholders; Save / Cancel.
- Apply semantics: routing only takes effect on a fresh Xray instance. On **Save while connected** → persist, then reconnect the current server via the existing `doConnect` path so the new routing applies immediately. **While disconnected** → just persist.
- `currentBypass()` reads the widgets/store under the existing `mu` lock, same as `readSettings()`.

## 5. Edge cases / error handling
- **Geo missing at connect** (Iran/Private active): `ensureGeoAssets()` re-extracts. If extraction fails, `core.New` errors → surfaced through the existing `setStatus("Error", …)` path.
- **Empty / whitespace / `#` lines** in custom fields: ignored by `parseList`.
- **Invalid custom token**: passed through; Xray validates at Start; failure shows the existing error status. (Optional lightweight client-side validation later.)
- **Master off / everything empty** → `buildRouting` returns `nil` → exact current behavior.
- **Concurrency**: single Xray instance; `XRAY_LOCATION_ASSET` set once, process-wide — safe.

## 6. Testing
- Unit `buildRouting` table tests: each preset combination → expected rules slice (and that disabled → `nil`).
- Unit `parseList`: comments, blanks, surrounding whitespace, CRLF.
- Unit `bypassRules` JSON round-trip + first-run default-seed detection (key absent → seeded; key present with all-false → respected).
- Manual/integration: build; connect with Iran on; `curl --socks5-hostname 127.0.0.1:10808 https://<an .ir site>` returns the **real** IP (direct) while a foreign site returns the **proxy** IP. Confirm geo files extracted and env var set. Confirm graceful error if the geo dir is deleted.
- `tools/gengeo`: after running once, a smoke test loads a config referencing `geoip:ir` + `geosite:category-ir` through `core.New` to prove the trimmed/lite files parse.

## 7. File-by-file change list
**New:** `tools/gengeo/main.go`, `geo.go`, `routing.go`, `routing_test.go`, `assets/geo/geoip.dat`, `assets/geo/geosite.dat`.
**Edit:** `config.go` (routing param), `engine.go` (Start signature + pass routing), `main.go` (nil routing), `persist.go` (`store.Bypass` + seed), `gui.go` (bypass dialog, wire into `doConnect`/`startMonitor`, `ensureGeoAssets` at startup), `README.md` (feature + gengeo note), `.gitignore` (ensure `assets/geo/*.dat` are **not** ignored), `go.mod`/`go.sum` (promote `google.golang.org/protobuf` to direct if needed).

## 8. Risks / open items
- `geosite-lite`'s `CATEGORY-IR` coverage depends on the community repo; acceptable — refresh via `gengeo`.
- Trimmed `geoip.dat` must round-trip cleanly through xray's protobuf — validated by the smoke test.
- `AsIs` domainStrategy: a purely-IP connection to an Iranian host that isn't caught by `geoip:ir` at connect time won't bypass; coverage is good in practice because most connects are by-domain + sniff. Revisit `IPIfNonMatch` only if real-world gaps appear.
