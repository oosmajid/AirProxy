package main

import (
	"bytes"
	_ "embed"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	_ "image/png"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	xdraw "golang.org/x/image/draw"
)

// آیکون کوچک از‌پیش‌decode‌شده برای ردیف‌ها (تا برای هر ردیف PNG بزرگ دوباره decode نشود).
func smallIcon() image.Image {
	src, _, err := image.Decode(bytes.NewReader(iconPNG))
	if err != nil {
		return image.NewRGBA(image.Rect(0, 0, 1, 1))
	}
	dst := image.NewRGBA(image.Rect(0, 0, 52, 52))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), xdraw.Over, nil)
	return dst
}

//go:embed icon.png
var iconPNG []byte

const appID = "com.airproxy.app"

// migratePrefs دیتای ذخیره‌شدهٔ نسخهٔ قبلی را به شناسهٔ جدید منتقل می‌کند (یک‌بار).
func migratePrefs(oldID, newID string) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	base := filepath.Join(home, "Library", "Preferences", "fyne")
	oldF := filepath.Join(base, oldID, "preferences.json")
	newDir := filepath.Join(base, newID)
	newF := filepath.Join(newDir, "preferences.json")
	if _, err := os.Stat(newF); err == nil {
		return // از قبل وجود دارد
	}
	data, err := os.ReadFile(oldF)
	if err != nil {
		return
	}
	if os.MkdirAll(newDir, 0o755) == nil {
		_ = os.WriteFile(newF, data, 0o644)
	}
}

func runGUI() {
	migratePrefs("com.local.v2proxy", appID) // انتقال دیتای نسخهٔ قبلی (V2Proxy)
	a := app.NewWithID(appID)
	a.Settings().SetTheme(&appTheme{})
	icon := fyne.NewStaticResource("icon.png", iconPNG)
	a.SetIcon(icon)
	rowIcon := smallIcon()
	prefs := a.Preferences()
	st := loadStore(prefs)

	// فایل‌های geo را استخراج کن و XRAY_LOCATION_ASSET را ست کن تا قوانین
	// bypass ژئویی (geoip:ir / geosite:category-ir) کار کنند.
	if _, err := ensureGeoAssets(); err != nil {
		fmt.Println("geo assets:", err) // بدون geo، bypass ژئویی کار نمی‌کند؛ ادامه بده.
	}

	w := a.NewWindow("AirProxy")
	w.SetIcon(icon)
	w.Resize(fyne.NewSize(420, 760))

	eng := &Engine{}

	var (
		mu           sync.Mutex
		sources      = st.Sources
		configs      = st.Configs
		bypass       = st.Bypass
		adv          = st.Adv
		selectedRaw  string
		connectedRaw string
		monGen       int64
		collapsed    = map[string]bool{}
		rowsByRaw    = map[string]*serverRow{}
	)
	for i := range configs {
		configs[i].Latency = "n/a"
		// backfill برای دیتای ذخیره‌شدهٔ نسخه‌های قبلی
		if configs[i].Proto == "" {
			configs[i].Proto = protoOf(configs[i].Raw)
		}
		if configs[i].Group == "" && len(sources) > 0 {
			configs[i].Group = sources[0]
		}
	}

	// ---------- forward decls ----------
	var rebuild func()
	var showRowMenu func(raw string, e *fyne.PointEvent)
	var doConnect func(raw string)
	var doDisconnect func()

	// ---------- status / power ----------
	statusText := canvas.NewText("Disconnected", colFgDim)
	statusText.TextSize = 15
	statusText.Alignment = fyne.TextAlignCenter
	statusText.TextStyle = fyne.TextStyle{Bold: true}
	subStatus := canvas.NewText("Select a server and tap the button", colFgDim)
	subStatus.TextSize = 11.5
	subStatus.Alignment = fyne.TextAlignCenter

	var powerBtn *powerButton
	setStatus := func(main, sub string, c interface {
		RGBA() (uint32, uint32, uint32, uint32)
	}, pstate int) {
		fyne.Do(func() {
			statusText.Text = main
			statusText.Color = c
			statusText.Refresh()
			if sub != "" {
				subStatus.Text = sub
			}
			subStatus.Refresh()
			powerBtn.setState(pstate)
		})
	}

	// ---------- settings widgets (in dialog) ----------
	listenEntry := widget.NewEntry()
	listenEntry.SetText(st.Listen)
	socksEntry := widget.NewEntry()
	socksEntry.SetText(strconv.Itoa(st.Socks))
	httpEntry := widget.NewEntry()
	httpEntry.SetText(strconv.Itoa(st.HTTP))
	rotateCheck := widget.NewCheck("Auto-rotate if the connection drops", nil)
	rotateCheck.SetChecked(st.Rotate)

	readSettings := func() (string, int, int, error) {
		listen := strings.TrimSpace(listenEntry.Text)
		if listen == "" {
			listen = "127.0.0.1"
		}
		socks, err := strconv.Atoi(strings.TrimSpace(socksEntry.Text))
		if err != nil || socks <= 0 || socks > 65535 {
			return "", 0, 0, fmt.Errorf("invalid SOCKS port")
		}
		httpP := 0
		if h := strings.TrimSpace(httpEntry.Text); h != "" {
			if hp, e := strconv.Atoi(h); e == nil {
				httpP = hp
			}
		}
		return listen, socks, httpP, nil
	}

	persist := func() {
		l, s, h, _ := readSettings()
		mu.Lock()
		stt := store{
			Sources: append([]string{}, sources...),
			Configs: append([]cfgItem{}, configs...),
			Listen:  l, Socks: s, HTTP: h, Rotate: rotateCheck.Checked,
			Bypass: bypass,
			Adv:    adv,
		}
		mu.Unlock()
		saveStore(prefs, stt)
	}

	// currentAdv نسخهٔ فعلی تنظیمات پیشرفته را به‌صورت thread-safe برمی‌گرداند.
	currentAdv := func() advanced {
		mu.Lock()
		defer mu.Unlock()
		return adv
	}

	// ---------- groups list ----------
	groupsBox := container.NewVBox()

	groupItems := func(src string) []cfgItem {
		var out []cfgItem
		for _, c := range configs {
			if c.Group == src {
				out = append(out, c)
			}
		}
		return out
	}

	updateRowStyles := func() {
		for raw, r := range rowsByRaw {
			r.setSelected(raw == selectedRaw && raw != connectedRaw)
			r.setConnected(raw == connectedRaw)
		}
	}

	selectRaw := func(raw string) {
		mu.Lock()
		selectedRaw = raw
		mu.Unlock()
		updateRowStyles()
	}

	// ping helpers
	setLatData := func(raw, res string) { // فقط دیتا (thread-safe)
		mu.Lock()
		for i := range configs {
			if configs[i].Raw == raw {
				configs[i].Latency = res
			}
		}
		mu.Unlock()
	}
	applyLatUI := func(raw, res string) { // فقط UI (باید روی رشتهٔ اصلی)
		if r := rowsByRaw[raw]; r != nil {
			r.setLatency(res)
		}
	}
	// pingRaws از رشتهٔ اصلی (UI) صدا زده می‌شود.
	pingRaws := func(raws []string) {
		for _, raw := range raws {
			setLatData(raw, "...")
			applyLatUI(raw, "...") // اسپینر روشن
		}
		pingAdv := currentAdv()
		go func() {
			// هر پینگ یک نمونهٔ موقتِ Xray بالا می‌آورد، پس همزمانی را پایین‌تر نگه می‌داریم.
			sem := make(chan struct{}, 6)
			var wg sync.WaitGroup
			for _, raw := range raws {
				wg.Add(1)
				sem <- struct{}{}
				go func(rw string) {
					defer wg.Done()
					defer func() { <-sem }()
					d, err := realPing(rw, 8*time.Second, pingAdv)
					res := "timeout"
					if err == nil {
						res = fmt.Sprintf("%d ms", d.Milliseconds())
					}
					setLatData(rw, res)
					fyne.Do(func() { applyLatUI(rw, res) })
				}(raw)
			}
			wg.Wait()
		}()
	}

	sortGroupByPing := func(src string) {
		mu.Lock()
		sort.SliceStable(configs, func(i, j int) bool {
			if configs[i].Group != src || configs[j].Group != src {
				return false // فقط داخل همین گروه را جابه‌جا کن
			}
			return latVal(configs[i].Latency) < latVal(configs[j].Latency)
		})
		mu.Unlock()
		rebuild()
		persist()
	}

	// fetch a source and replace its items
	loadSource := func(src string, after func()) {
		go func() {
			var items []cfgItem
			var links []string
			if isSubURL(src) {
				if ls, err := fetchSub(src); err == nil {
					links = ls
				}
			} else {
				for _, ln := range strings.Split(src, "\n") {
					ln = strings.TrimSpace(ln)
					if strings.Contains(ln, "://") {
						links = append(links, ln)
					}
				}
			}
			for _, l := range links {
				items = append(items, cfgItem{Raw: l, Name: linkName(l), Proto: protoOf(l), Group: src, Latency: "n/a"})
			}
			fyne.Do(func() {
				mu.Lock()
				// حذف آیتم‌های قبلی این گروه و افزودن جدیدها
				var kept []cfgItem
				for _, c := range configs {
					if c.Group != src {
						kept = append(kept, c)
					}
				}
				configs = append(kept, items...)
				mu.Unlock()
				rebuild()
				persist()
				subStatus.Text = fmt.Sprintf("%s: %d configs", prettyGroup(src), len(items))
				subStatus.Refresh()
				if after != nil {
					after()
				}
			})
		}()
	}

	// row context menu
	showRowMenu = func(raw string, e *fyne.PointEvent) {
		items := []menuItem{
			{"Copy URL", theme.ContentCopyIcon(), func() {
				a.Clipboard().SetContent(raw)
				subStatus.Text = "URL copied."
				subStatus.Refresh()
			}},
			{"Ping", theme.SearchIcon(), func() { pingRaws([]string{raw}) }},
			{"Edit", theme.DocumentCreateIcon(), func() {
				entry := widget.NewMultiLineEntry()
				entry.SetText(raw)
				entry.Wrapping = fyne.TextWrapBreak
				d := dialog.NewCustomConfirm("Edit config", "Save", "Cancel",
					container.New(&fixedHeight{130}, entry), func(ok bool) {
						if !ok {
							return
						}
						nv := strings.TrimSpace(entry.Text)
						if nv == "" {
							return
						}
						mu.Lock()
						for i := range configs {
							if configs[i].Raw == raw {
								configs[i].Raw = nv
								configs[i].Name = linkName(nv)
								configs[i].Proto = protoOf(nv)
								configs[i].Latency = "n/a"
							}
						}
						mu.Unlock()
						rebuild()
						persist()
					}, w)
				d.Resize(fyne.NewSize(380, 230))
				d.Show()
			}},
			{"Delete", theme.DeleteIcon(), func() {
				mu.Lock()
				var kept []cfgItem
				for _, c := range configs {
					if c.Raw != raw {
						kept = append(kept, c)
					}
				}
				configs = kept
				if selectedRaw == raw {
					selectedRaw = ""
				}
				mu.Unlock()
				rebuild()
				persist()
			}},
		}
		showMenu(w.Canvas(), e.AbsolutePosition, items)
	}

	// build a group card
	buildGroup := func(src string) fyne.CanvasObject {
		items := groupItems(src)
		isCollapsed := collapsed[src]

		chevTxt := "▾"
		if isCollapsed {
			chevTxt = "▸"
		}
		chev := widget.NewButton(chevTxt, func() {
			collapsed[src] = !collapsed[src]
			rebuild()
		})
		chev.Importance = widget.LowImportance

		title := canvas.NewText(prettyGroup(src), colFg)
		title.TextSize = 14
		title.TextStyle = fyne.TextStyle{Bold: true}

		pingBtn := widget.NewButtonWithIcon("", theme.SearchIcon(), func() {
			var raws []string
			for _, it := range items {
				raws = append(raws, it.Raw)
			}
			pingRaws(raws)
		})
		pingBtn.Importance = widget.LowImportance

		moreBtn := widget.NewButtonWithIcon("", theme.MoreHorizontalIcon(), nil)
		moreBtn.Importance = widget.LowImportance
		moreBtn.OnTapped = func() {
			var groupRaws []string
			for _, it := range groupItems(src) {
				groupRaws = append(groupRaws, it.Raw)
			}
			menu := []menuItem{
				{"Ping all", theme.SearchIcon(), func() { pingRaws(groupRaws) }},
				{"Sort by ping", theme.MenuDropDownIcon(), func() { sortGroupByPing(src) }},
				{"Reload", theme.ViewRefreshIcon(), func() { loadSource(src, nil) }},
				{"Remove source", theme.DeleteIcon(), func() {
					mu.Lock()
					var ns []string
					for _, s := range sources {
						if s != src {
							ns = append(ns, s)
						}
					}
					sources = ns
					var kept []cfgItem
					for _, c := range configs {
						if c.Group != src {
							kept = append(kept, c)
						}
					}
					configs = kept
					mu.Unlock()
					rebuild()
					persist()
				}},
			}
			pos := fyne.CurrentApp().Driver().AbsolutePositionForObject(moreBtn)
			showMenu(w.Canvas(), pos.Add(fyne.NewPos(-110, 30)), menu)
		}

		header := container.NewBorder(nil, nil,
			container.NewHBox(chev, title),
			container.NewHBox(pingBtn, moreBtn),
		)

		vbox := container.NewVBox(header)
		if !isCollapsed {
			for _, it := range items {
				raw := it.Raw
				row := newServerRow(rowIcon, it.Name, it.Proto, it.Latency,
					func() { selectRaw(raw) },
					func(e *fyne.PointEvent) { showRowMenu(raw, e) },
				)
				rowsByRaw[raw] = row
				row.setSelected(raw == selectedRaw && raw != connectedRaw)
				row.setConnected(raw == connectedRaw)
				sep := canvas.NewRectangle(colSeprator)
				sep.SetMinSize(fyne.NewSize(0, 1))
				vbox.Add(sep)
				vbox.Add(row)
			}
		}

		bg := canvas.NewRectangle(colCard)
		bg.CornerRadius = 14
		return container.NewStack(bg, container.NewPadded(vbox))
	}

	rebuild = func() {
		groupsBox.Objects = nil
		rowsByRaw = map[string]*serverRow{}
		mu.Lock()
		srcs := append([]string{}, sources...)
		hasConfigs := len(configs) > 0
		mu.Unlock()
		if len(srcs) == 0 && !hasConfigs {
			hint := widget.NewLabelWithStyle("No servers yet.\nTap  +  to add a subscription or a config link.",
				fyne.TextAlignCenter, fyne.TextStyle{Italic: true})
			groupsBox.Add(container.NewPadded(hint))
			groupsBox.Refresh()
			return
		}
		for _, s := range srcs {
			groupsBox.Add(buildGroup(s))
			groupsBox.Add(layoutSpacer(8))
		}
		groupsBox.Refresh()
	}

	// ---------- connect / rotation ----------
	allRaws := func() []string {
		mu.Lock()
		defer mu.Unlock()
		out := make([]string, len(configs))
		for i := range configs {
			out[i] = configs[i].Raw
		}
		return out
	}
	nameOf := func(raw string) string {
		mu.Lock()
		defer mu.Unlock()
		for _, c := range configs {
			if c.Raw == raw {
				return c.Name
			}
		}
		return ""
	}
	// currentBypass نسخهٔ فعلی قوانین bypass را به‌صورت thread-safe برمی‌گرداند.
	currentBypass := func() bypassRules {
		mu.Lock()
		defer mu.Unlock()
		return bypass
	}

	startMonitor := func(gen int64, listen string, socks, httpP int) {
		go func() {
			fails := 0
			for {
				time.Sleep(5 * time.Second)
				if atomic.LoadInt64(&monGen) != gen {
					return
				}
				if _, err := proxyHealth(listen, socks, 6*time.Second); err == nil {
					fails = 0
					continue
				}
				fails++
				if fails < 2 {
					continue
				}
				if !rotateCheck.Checked {
					setStatus("Unstable", "Connection down (auto-rotate off)", colRed, 2)
					fails = 0
					continue
				}
				setStatus("Rotating…", "Switching to another server", colAccent, 1)
				raws := allRaws()
				mu.Lock()
				curRaw := connectedRaw
				mu.Unlock()
				start := 0
				for i, r := range raws {
					if r == curRaw {
						start = i
						break
					}
				}
				switched := false
				for off := 1; off <= len(raws); off++ {
					if atomic.LoadInt64(&monGen) != gen {
						return
					}
					nr := raws[(start+off)%len(raws)]
					if nr == curRaw {
						continue
					}
					if err := eng.Start(nr, listen, socks, httpP, buildRouting(currentBypass()), currentAdv()); err != nil {
						continue
					}
					if _, err := proxyHealth(listen, socks, 6*time.Second); err != nil {
						continue
					}
					mu.Lock()
					connectedRaw = nr
					mu.Unlock()
					fyne.Do(updateRowStyles)
					setStatus("Connected", fmt.Sprintf("%s  •  auto-rotated", nameOf(nr)), colGreen, 2)
					fails = 0
					switched = true
					break
				}
				if !switched {
					eng.Stop()
					atomic.AddInt64(&monGen, 1)
					mu.Lock()
					connectedRaw = ""
					mu.Unlock()
					fyne.Do(updateRowStyles)
					setStatus("Disconnected", "No working server found", colRed, 0)
					return
				}
			}
		}()
	}

	doConnect = func(raw string) {
		listen, socks, httpP, err := readSettings()
		if err != nil {
			dialog.ShowError(err, w)
			return
		}
		persist()
		setStatus("Connecting…", nameOf(raw), colAccent, 1)
		go func() {
			if err := eng.Start(raw, listen, socks, httpP, buildRouting(currentBypass()), currentAdv()); err != nil {
				setStatus("Error", err.Error(), colRed, 0)
				return
			}
			gen := atomic.AddInt64(&monGen, 1)
			mu.Lock()
			connectedRaw = raw
			mu.Unlock()
			fyne.Do(updateRowStyles)
			sub := fmt.Sprintf("%s  •  SOCKS5 %s:%d", nameOf(raw), listen, socks)
			if httpP > 0 {
				sub += fmt.Sprintf("  •  HTTP %d", httpP)
			}
			setStatus("Connected", sub, colGreen, 2)
			startMonitor(gen, listen, socks, httpP)
		}()
	}

	doDisconnect = func() {
		atomic.AddInt64(&monGen, 1)
		eng.Stop()
		mu.Lock()
		connectedRaw = ""
		mu.Unlock()
		updateRowStyles()
		setStatus("Disconnected", "", colFgDim, 0)
	}

	onPower := func() {
		mu.Lock()
		sel := selectedRaw
		conn := connectedRaw
		if sel == "" && len(configs) > 0 {
			sel = configs[0].Raw
			selectedRaw = sel
		}
		mu.Unlock()
		updateRowStyles()

		if eng.Running() {
			if sel != "" && sel != conn {
				doConnect(sel) // سوییچ به سرور انتخاب‌شده
			} else {
				doDisconnect()
			}
			return
		}
		if sel == "" {
			subStatus.Text = "Add and select a server first."
			subStatus.Refresh()
			return
		}
		doConnect(sel)
	}
	powerBtn = newPowerButton(onPower)

	// ---------- top toolbar ----------
	// showBypassDialog دیالوگ split-tunneling را نشان می‌دهد. هر بار ویجت‌ها را از
	// روی مقدار فعلی bypass می‌سازد تا Cancel چیزی را تغییر ندهد.
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

	// showAdvancedDialog تنظیمات هستهٔ سبک v2rayN (Fragment / ECH / Mux / TLS / …) را نشان می‌دهد.
	showAdvancedDialog := func() {
		cur := currentAdv()
		entry := func(val, ph string) *widget.Entry {
			e := widget.NewEntry()
			e.SetText(val)
			e.SetPlaceHolder(ph)
			return e
		}

		frag := widget.NewCheck("Enable Fragment (split TLS ClientHello)", nil)
		frag.SetChecked(cur.Fragment)
		fragPackets := entry(cur.FragPackets, "tlshello  or  1-3")
		fragLength := entry(cur.FragLength, "100-200")
		fragInterval := entry(cur.FragInterval, "10-20")

		mux := widget.NewCheck("Enable Mux (mux.cool)", nil)
		mux.SetChecked(cur.Mux)
		muxConc := entry(strconv.Itoa(orInt(cur.MuxConcurrency, 8)), "8")

		fpNone := "(none)"
		fp := widget.NewSelect([]string{"chrome", "firefox", "safari", "ios", "android", "edge", "360", "qq", "random", "randomized", fpNone}, nil)
		fp.SetSelected(orDefault(cur.Fingerprint, fpNone))
		ech := entry(cur.ECH, "cloudflare-ech.com+https://1.1.1.1/dns-query")
		echDef := "(default)"
		echForce := widget.NewSelect([]string{echDef, "none", "half", "full"}, nil)
		echForce.SetSelected(orDefault(cur.ECHForce, echDef))

		tfo := widget.NewCheck("TCP Fast Open", nil)
		tfo.SetChecked(cur.TFO)
		ds := widget.NewSelect([]string{"AsIs", "IPIfNonMatch", "IPOnDemand"}, nil)
		ds.SetSelected(orDefault(cur.DomainStrategy, "AsIs"))
		bind := widget.NewCheck("Bypass system VPN (bind to physical interface)", nil)
		bind.SetChecked(cur.BindPhysical)
		vpnNote := widget.NewLabel("")
		if name, on := physicalInterface(); on {
			vpnNote.SetText(fmt.Sprintf("⚠︎ A system VPN is active — traffic will go via %s.", name))
		} else {
			vpnNote.SetText("No system VPN detected.")
		}
		vpnNote.Wrapping = fyne.TextWrapWord

		form := container.NewVBox(
			widget.NewLabelWithStyle("Fragment", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			frag,
			labeled("Packets", fragPackets),
			labeled("Length", fragLength),
			labeled("Interval (ms)", fragInterval),
			widget.NewSeparator(),
			widget.NewLabelWithStyle("TLS / ECH", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			labeled("Default uTLS fingerprint", fp),
			labeled("ECH config list / DNS (for all TLS configs)", ech),
			labeled("ECH force query", echForce),
			widget.NewSeparator(),
			widget.NewLabelWithStyle("Mux & network", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			mux,
			labeled("Mux concurrency", muxConc),
			tfo,
			labeled("Routing domain strategy", ds),
			bind,
			vpnNote,
		)
		d := dialog.NewCustomConfirm("Advanced (v2rayN-style)", "Save", "Cancel",
			container.New(&fixedHeight{480}, container.NewVScroll(form)), func(ok bool) {
				if !ok {
					return
				}
				n := defaultAdvanced()
				n.Fragment = frag.Checked
				n.FragPackets = orDefault(strings.TrimSpace(fragPackets.Text), n.FragPackets)
				n.FragLength = orDefault(strings.TrimSpace(fragLength.Text), n.FragLength)
				n.FragInterval = orDefault(strings.TrimSpace(fragInterval.Text), n.FragInterval)
				n.Mux = mux.Checked
				n.MuxConcurrency = orInt(atoi(muxConc.Text), 8)
				n.Fingerprint = fp.Selected
				if n.Fingerprint == fpNone {
					n.Fingerprint = ""
				}
				n.ECH = strings.TrimSpace(ech.Text)
				n.ECHForce = echForce.Selected
				if n.ECHForce == echDef {
					n.ECHForce = ""
				}
				n.TFO = tfo.Checked
				n.DomainStrategy = ds.Selected
				n.BindPhysical = bind.Checked
				mu.Lock()
				adv = n
				conn := connectedRaw
				mu.Unlock()
				persist()
				if eng.Running() && conn != "" {
					doConnect(conn)
				}
			}, w)
		d.Resize(fyne.NewSize(420, 600))
		d.Show()
	}

	gearBtn := widget.NewButtonWithIcon("", theme.SettingsIcon(), func() {
		bypassBtn := widget.NewButtonWithIcon("Bypass / split-tunneling…", theme.MailForwardIcon(), showBypassDialog)
		bypassBtn.Importance = widget.LowImportance
		advBtn := widget.NewButtonWithIcon("Advanced: Fragment / ECH / Mux…", theme.SettingsIcon(), showAdvancedDialog)
		advBtn.Importance = widget.LowImportance
		form := container.NewVBox(
			labeled("IP", listenEntry),
			labeled("SOCKS5 port", socksEntry),
			labeled("HTTP port (optional)", httpEntry),
			layoutSpacer(4),
			rotateCheck,
			layoutSpacer(8),
			bypassBtn,
			advBtn,
			layoutSpacer(4),
		)
		d := dialog.NewCustomConfirm("Settings", "Save", "Close", form, func(bool) { persist() }, w)
		d.Resize(fyne.NewSize(380, 420))
		d.Show()
	})
	gearBtn.Importance = widget.LowImportance

	// addSource یک منبع جدید (ساب/لینک/لینک ssh) را اضافه و بارگذاری می‌کند.
	addSource := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		mu.Lock()
		sources = append(sources, s)
		mu.Unlock()
		persist()
		rebuild()
		loadSource(s, nil)
	}

	showAddSource := func() {
		entry := widget.NewMultiLineEntry()
		entry.SetPlaceHolder("Subscription URL (https://…) or a config link\n(vmess / vless / trojan / ss / hy2 / wireguard / socks / ssh)")
		entry.Wrapping = fyne.TextWrapBreak
		d := dialog.NewCustomConfirm("Add source", "Add", "Cancel",
			container.New(&fixedHeight{120}, entry), func(ok bool) {
				if ok {
					addSource(entry.Text)
				}
			}, w)
		d.Resize(fyne.NewSize(380, 220))
		d.Show()
	}

	showSSHForm := func() {
		nameE := widget.NewEntry()
		nameE.SetPlaceHolder("My VPS (optional)")
		hostE := widget.NewEntry()
		hostE.SetPlaceHolder("example.com or 1.2.3.4")
		portE := widget.NewEntry()
		portE.SetText("22")
		userE := widget.NewEntry()
		userE.SetPlaceHolder("root")
		passE := widget.NewPasswordEntry()
		passE.SetPlaceHolder("password (or use a private key)")
		keyE := widget.NewMultiLineEntry()
		keyE.SetPlaceHolder("-----BEGIN OPENSSH PRIVATE KEY----- (optional)")
		keyE.Wrapping = fyne.TextWrapBreak
		phraseE := widget.NewPasswordEntry()
		phraseE.SetPlaceHolder("key passphrase (optional)")

		loadKeyBtn := widget.NewButtonWithIcon("Load key file…", theme.FolderOpenIcon(), func() {
			fd := dialog.NewFileOpen(func(rc fyne.URIReadCloser, err error) {
				if err != nil || rc == nil {
					return
				}
				defer rc.Close()
				if data, err := io.ReadAll(rc); err == nil {
					keyE.SetText(string(data))
				}
			}, w)
			fd.Show()
		})
		loadKeyBtn.Importance = widget.LowImportance

		form := container.NewVBox(
			labeled("Name", nameE),
			labeled("Host", hostE),
			labeled("Port", portE),
			labeled("Username", userE),
			labeled("Password", passE),
			labeled("Private key", container.NewBorder(nil, loadKeyBtn, nil, nil,
				container.New(&fixedHeight{90}, keyE))),
			labeled("Key passphrase", phraseE),
		)
		d := dialog.NewCustomConfirm("Add SSH tunnel", "Add", "Cancel",
			container.New(&fixedHeight{420}, container.NewVScroll(form)), func(ok bool) {
				if !ok {
					return
				}
				link, err := buildSSHLink(nameE.Text, hostE.Text, portE.Text,
					userE.Text, passE.Text, keyE.Text, phraseE.Text)
				if err != nil {
					dialog.ShowError(err, w)
					return
				}
				addSource(link)
			}, w)
		d.Resize(fyne.NewSize(400, 560))
		d.Show()
	}

	var addBtn *widget.Button
	addBtn = widget.NewButtonWithIcon("", theme.ContentAddIcon(), func() {
		menu := []menuItem{
			{"Subscription / config link", theme.ContentAddIcon(), showAddSource},
			{"SSH tunnel", theme.ComputerIcon(), showSSHForm},
		}
		pos := fyne.CurrentApp().Driver().AbsolutePositionForObject(addBtn)
		showMenu(w.Canvas(), pos.Add(fyne.NewPos(-180, 34)), menu)
	})
	addBtn.Importance = widget.LowImportance

	titleTop := canvas.NewText("AirProxy", colFg)
	titleTop.TextSize = 17
	titleTop.TextStyle = fyne.TextStyle{Bold: true}
	titleTop.Alignment = fyne.TextAlignCenter

	toolbar := container.NewBorder(nil, nil, gearBtn, addBtn, container.NewCenter(titleTop))

	powerArea := container.NewVBox(
		layoutSpacer(10),
		container.NewCenter(container.New(&fixedSize{170, 170}, powerBtn)),
		layoutSpacer(8),
		statusText,
		subStatus,
		layoutSpacer(10),
	)

	listScroll := container.NewVScroll(container.NewPadded(groupsBox))

	content := container.NewBorder(
		container.NewVBox(toolbar, powerArea, widget.NewSeparator()),
		nil, nil, nil,
		listScroll,
	)

	w.SetOnClosed(func() { atomic.AddInt64(&monGen, 1); eng.Stop(); persist() })
	w.SetContent(content)
	rebuild()
	w.ShowAndRun()
}

// ---- animated menu ----

type menuItem struct {
	Label  string
	Icon   fyne.Resource
	Action func()
}

func transparentNRGBA(c color.NRGBA) color.NRGBA { c.A = 0; return c }

// showMenu یک منوی شناور با fade-in/out سریع و ظریف نشان می‌دهد.
func showMenu(c fyne.Canvas, at fyne.Position, items []menuItem) {
	var pop *widget.PopUp
	overlay := canvas.NewRectangle(colCard)

	closeWith := func(then func()) {
		anim := canvas.NewColorRGBAAnimation(transparentNRGBA(colCard), colCard, 70*time.Millisecond,
			func(cc color.Color) { overlay.FillColor = cc; overlay.Refresh() })
		anim.Curve = fyne.AnimationEaseIn
		anim.Start()
		go func() {
			time.Sleep(80 * time.Millisecond)
			fyne.Do(func() {
				if pop != nil {
					pop.Hide()
				}
				if then != nil {
					then()
				}
			})
		}()
	}

	rows := container.NewVBox()
	for _, it := range items {
		it := it
		b := widget.NewButtonWithIcon(it.Label, it.Icon, func() { closeWith(it.Action) })
		b.Alignment = widget.ButtonAlignLeading
		b.Importance = widget.LowImportance
		rows.Add(b)
	}

	card := canvas.NewRectangle(colCard)
	card.CornerRadius = 10
	content := container.NewStack(card, container.NewPadded(rows), overlay)
	pop = widget.NewPopUp(content, c)
	pop.ShowAtPosition(at)

	open := canvas.NewColorRGBAAnimation(colCard, transparentNRGBA(colCard), 110*time.Millisecond,
		func(cc color.Color) { overlay.FillColor = cc; overlay.Refresh() })
	open.Curve = fyne.AnimationEaseOut
	open.Start()
}

// ---- helpers ----

func labeled(label string, field fyne.CanvasObject) fyne.CanvasObject {
	t := canvas.NewText(label, colFgDim)
	t.TextSize = 12
	return container.NewVBox(t, field)
}

// buildSSHLink فیلدهای فرم را به یک لینک ssh:// قابل‌ذخیره تبدیل می‌کند.
// کلید خصوصی و passphrase به‌صورت base64 در query کدگذاری می‌شوند.
func buildSSHLink(name, host, portStr, user, pass, key, passphrase string) (string, error) {
	host = strings.TrimSpace(host)
	user = strings.TrimSpace(user)
	if host == "" {
		return "", fmt.Errorf("Host is required")
	}
	if user == "" {
		return "", fmt.Errorf("Username is required")
	}
	port := strings.TrimSpace(portStr)
	if port == "" {
		port = "22"
	}
	if pass == "" && strings.TrimSpace(key) == "" {
		return "", fmt.Errorf("Provide a password or a private key")
	}

	u := &url.URL{Scheme: "ssh", Host: net.JoinHostPort(host, port)}
	if pass != "" {
		u.User = url.UserPassword(user, pass)
	} else {
		u.User = url.User(user)
	}
	q := url.Values{}
	if k := strings.TrimSpace(key); k != "" {
		q.Set("pk", base64.RawURLEncoding.EncodeToString([]byte(k)))
	}
	if passphrase != "" {
		q.Set("pass", base64.RawURLEncoding.EncodeToString([]byte(passphrase)))
	}
	u.RawQuery = q.Encode()
	if n := strings.TrimSpace(name); n != "" {
		u.Fragment = n
	}
	return u.String(), nil
}

func latVal(s string) int {
	s = strings.TrimSuffix(strings.TrimSpace(s), " ms")
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return 1 << 30
}

func isSubURL(s string) bool {
	s = strings.TrimSpace(s)
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

// fixedHeight ارتفاع ثابت، عرض کشسان.
type fixedHeight struct{ h float32 }

func (f *fixedHeight) MinSize(objs []fyne.CanvasObject) fyne.Size {
	w := float32(0)
	for _, o := range objs {
		if o.MinSize().Width > w {
			w = o.MinSize().Width
		}
	}
	return fyne.NewSize(w, f.h)
}
func (f *fixedHeight) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	for _, o := range objs {
		o.Resize(fyne.NewSize(size.Width, f.h))
		o.Move(fyne.NewPos(0, 0))
	}
}

// fixedSize اندازهٔ کاملاً ثابت.
type fixedSize struct{ w, h float32 }

func (f *fixedSize) MinSize([]fyne.CanvasObject) fyne.Size { return fyne.NewSize(f.w, f.h) }
func (f *fixedSize) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	for _, o := range objs {
		o.Resize(fyne.NewSize(f.w, f.h))
		o.Move(fyne.NewPos(0, 0))
	}
}
