package main

import (
	"bytes"
	_ "embed"
	"fmt"
	"image"
	"image/color"
	_ "image/png"
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

func runGUI() {
	a := app.NewWithID("com.local.v2proxy")
	a.Settings().SetTheme(&appTheme{})
	icon := fyne.NewStaticResource("icon.png", iconPNG)
	a.SetIcon(icon)
	rowIcon := smallIcon()
	prefs := a.Preferences()
	st := loadStore(prefs)

	w := a.NewWindow("V2Proxy")
	w.SetIcon(icon)
	w.Resize(fyne.NewSize(420, 760))

	eng := &Engine{}

	var (
		mu           sync.Mutex
		sources      = st.Sources
		configs      = st.Configs
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
	setStatus := func(main, sub string, c interface{ RGBA() (uint32, uint32, uint32, uint32) }, pstate int) {
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
		}
		mu.Unlock()
		saveStore(prefs, stt)
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
		go func() {
			sem := make(chan struct{}, 16)
			var wg sync.WaitGroup
			for _, raw := range raws {
				wg.Add(1)
				sem <- struct{}{}
				go func(rw string) {
					defer wg.Done()
					defer func() { <-sem }()
					d, err := tcpPing(rw, 5*time.Second)
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
					if err := eng.Start(nr, listen, socks, httpP); err != nil {
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
			if err := eng.Start(raw, listen, socks, httpP); err != nil {
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
	gearBtn := widget.NewButtonWithIcon("", theme.SettingsIcon(), func() {
		form := container.NewVBox(
			labeled("IP", listenEntry),
			labeled("SOCKS5 port", socksEntry),
			labeled("HTTP port (optional)", httpEntry),
			layoutSpacer(4),
			rotateCheck,
			layoutSpacer(4),
		)
		d := dialog.NewCustomConfirm("Settings", "Save", "Close", form, func(bool) { persist() }, w)
		d.Resize(fyne.NewSize(380, 340))
		d.Show()
	})
	gearBtn.Importance = widget.LowImportance

	addBtn := widget.NewButtonWithIcon("", theme.ContentAddIcon(), func() {
		entry := widget.NewMultiLineEntry()
		entry.SetPlaceHolder("Subscription URL (https://…) or a config link\n(vmess / vless / trojan / ss)")
		entry.Wrapping = fyne.TextWrapBreak
		d := dialog.NewCustomConfirm("Add source", "Add", "Cancel",
			container.New(&fixedHeight{120}, entry), func(ok bool) {
				if !ok {
					return
				}
				s := strings.TrimSpace(entry.Text)
				if s == "" {
					return
				}
				mu.Lock()
				sources = append(sources, s)
				mu.Unlock()
				persist()
				rebuild()
				loadSource(s, nil)
			}, w)
		d.Resize(fyne.NewSize(380, 220))
		d.Show()
	})
	addBtn.Importance = widget.LowImportance

	titleTop := canvas.NewText("V2Proxy", colFg)
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
