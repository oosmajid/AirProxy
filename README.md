# AirProxy

A simple, beautiful macOS app (written in Go) that turns a **V2Ray subscription or config link** into a **local SOCKS5 / HTTP proxy**. Only the chosen port is proxied — your whole system is **not** tunneled.

It embeds the [Xray-core](https://github.com/XTLS/Xray-core) engine, so it's a single self-contained binary — no separate `xray` install needed.

## Features

- 🔌 **Local proxy only** — runs SOCKS5 (and optional HTTP) on `127.0.0.1:10808`; the rest of the system is untouched.
- 📡 **Multiple subscriptions & links** — add several sources, grouped in the UI.
- 🧭 **Protocols**: VMess, VLESS (TLS / Reality / ws / grpc / tcp), Trojan, Shadowsocks.
- 📶 **Ping** — per-config or per-group latency test, with a loading spinner.
- ↕️ **Sort by ping** — fastest servers first.
- 🔁 **Auto-rotate** — if the active config drops, it automatically switches to another working one.
- 💾 **Persistent** — your sources, configs and settings are saved between launches.
- 🖱️ **Right-click a server** — Copy URL · Ping · Edit · Delete.
- 🎨 Light, Happ-inspired UI with the Vazirmatn font (Persian + Latin), animated menus.

## Install (macOS)

Download **`V2Proxy.dmg`** from the [Releases](../../releases), open it, and drag **V2Proxy** into **Applications**.

> The app is ad-hoc signed (no paid Apple certificate), so on first launch macOS may warn it's from an unidentified developer. Right-click the app → **Open**, or allow it in **System Settings → Privacy & Security**.

## Build from source

Requires Go 1.22+ and a C compiler (Xcode Command Line Tools).

```bash
go build -o v2proxy .
```

Package as a `.app` bundle and `.dmg`:

```bash
# 1. generate the icon (optional, icon.png is committed)
go run ./tools/genicon icon.png
sips -s format icns icon.png --out icon.icns   # or use iconutil

# 2. build the app bundle (see the steps in the build notes), then:
codesign --force --deep --sign - V2Proxy.app
hdiutil create -volname V2Proxy -srcfolder <staging> -ov -format UDZO V2Proxy.dmg
```

> Note: behind networks where `proxy.golang.org` is blocked, set a mirror:
> `export GOPROXY="https://goproxy.io,https://goproxy.cn,direct"`

## Command-line mode

The same binary also works headless:

```bash
# single config
./v2proxy --link "vless://…" --socks 10808

# from a subscription
./v2proxy --sub "https://…/subscribe?token=…" --list      # show configs
./v2proxy --sub "https://…/subscribe?token=…" --index 2 --socks 10808
```

| Flag | Default | Description |
|------|---------|-------------|
| `--link` | — | a single config link |
| `--sub` | — | subscription URL |
| `--index` | `0` | which config from the subscription |
| `--listen` | `127.0.0.1` | listen IP |
| `--socks` | `10808` | SOCKS5 port |
| `--http` | `0` | HTTP port (0 = off) |

## Test

```bash
curl --socks5-hostname 127.0.0.1:10808 https://api.ipify.org
```

If it returns the proxy server's IP, only that request went through the proxy — the rest of your system stays direct.

## License

Personal project. Bundles [Xray-core](https://github.com/XTLS/Xray-core) (MPL-2.0) and the [Vazirmatn](https://github.com/rastikerdar/vazirmatn) font (OFL).
