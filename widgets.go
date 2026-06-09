package main

import (
	"image"
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// ============ Power Button (دکمهٔ گرد بزرگ) ============

type powerButton struct {
	widget.BaseWidget
	onTap func()
	ring  *canvas.Circle
	mid   *canvas.Circle
	inner *canvas.Circle
	glyph *canvas.Text
}

func newPowerButton(onTap func()) *powerButton {
	p := &powerButton{onTap: onTap}
	p.ring = canvas.NewCircle(colRing)
	p.mid = canvas.NewCircle(color.NRGBA{0xF4, 0xF6, 0xF9, 0xFF})
	p.inner = canvas.NewCircle(colCard)
	p.glyph = canvas.NewText("⏻", colFgDim)
	p.glyph.TextSize = 46
	p.glyph.Alignment = fyne.TextAlignCenter
	p.glyph.TextStyle = fyne.TextStyle{Bold: true}
	p.ExtendBaseWidget(p)
	return p
}

func (p *powerButton) CreateRenderer() fyne.WidgetRenderer {
	c := container.NewWithoutLayout(p.ring, p.mid, p.inner, p.glyph)
	return &powerRenderer{p: p, c: c}
}

func (p *powerButton) Tapped(_ *fyne.PointEvent) {
	if p.onTap != nil {
		p.onTap()
	}
}

// setState: 0=off, 1=connecting, 2=on
func (p *powerButton) setState(s int) {
	switch s {
	case 2:
		p.glyph.Color = colGreen
		p.inner.FillColor = colConnBG
		p.ring.FillColor = color.NRGBA{0xCF, 0xEE, 0xDA, 0xFF}
	case 1:
		p.glyph.Color = colAccent
		p.inner.FillColor = colSelBG
		p.ring.FillColor = color.NRGBA{0xDD, 0xD7, 0xF7, 0xFF}
	default:
		p.glyph.Color = colFgDim
		p.inner.FillColor = colCard
		p.ring.FillColor = colRing
	}
	p.glyph.Refresh()
	p.inner.Refresh()
	p.ring.Refresh()
	p.mid.Refresh()
}

type powerRenderer struct {
	p *powerButton
	c *fyne.Container
}

func (r *powerRenderer) Destroy()                     {}
func (r *powerRenderer) Objects() []fyne.CanvasObject { return []fyne.CanvasObject{r.c} }
func (r *powerRenderer) MinSize() fyne.Size           { return fyne.NewSize(170, 170) }
func (r *powerRenderer) Refresh()                     { r.c.Refresh() }
func (r *powerRenderer) Layout(size fyne.Size) {
	d := size.Width
	if size.Height < d {
		d = size.Height
	}
	cx, cy := size.Width/2, size.Height/2
	place := func(o fyne.CanvasObject, diam float32) {
		o.Resize(fyne.NewSize(diam, diam))
		o.Move(fyne.NewPos(cx-diam/2, cy-diam/2))
	}
	place(r.p.ring, d)
	place(r.p.mid, d*0.86)
	place(r.p.inner, d*0.74)
	gs := r.p.glyph.MinSize()
	r.p.glyph.Resize(gs)
	r.p.glyph.Move(fyne.NewPos(cx-gs.Width/2, cy-gs.Height/2-2))
}

// ============ Server Row (ردیف سرور) ============

type serverRow struct {
	widget.BaseWidget
	bg          *canvas.Rectangle
	nameText    *canvas.Text
	protoText   *canvas.Text
	latText     *canvas.Text
	spinner     *widget.Activity
	icon        *canvas.Image
	selected    bool
	connected   bool
	onTap       func()
	onSecondary func(*fyne.PointEvent)
}

// newServerRow از یک تصویر از‌پیش‌decode‌شده استفاده می‌کند (بدون decode مجدد).
func newServerRow(img image.Image, name, proto, latency string, onTap func(), onSecondary func(*fyne.PointEvent)) *serverRow {
	r := &serverRow{onTap: onTap, onSecondary: onSecondary}
	r.bg = canvas.NewRectangle(color.Transparent)
	r.bg.CornerRadius = 10
	r.bg.SetMinSize(fyne.NewSize(0, 56))

	r.icon = canvas.NewImageFromImage(img)
	r.icon.FillMode = canvas.ImageFillContain
	r.icon.ScaleMode = canvas.ImageScaleSmooth
	r.icon.SetMinSize(fyne.NewSize(26, 26))

	r.nameText = canvas.NewText(name, colFg)
	r.nameText.TextSize = 14
	r.nameText.TextStyle = fyne.TextStyle{Bold: true}

	r.protoText = canvas.NewText(proto, colFgDim)
	r.protoText.TextSize = 11.5

	r.latText = canvas.NewText(latency, colFgDim)
	r.latText.TextSize = 12.5
	r.latText.Alignment = fyne.TextAlignTrailing

	r.spinner = widget.NewActivity()
	r.spinner.Hide()

	r.ExtendBaseWidget(r)
	return r
}

func (r *serverRow) CreateRenderer() fyne.WidgetRenderer {
	iconBox := container.New(&fixedSize{40, 56}, container.NewCenter(container.New(&fixedSize{26, 26}, r.icon)))
	textCol := container.NewVBox(layoutSpacer(10), r.nameText, r.protoText)

	chevron := canvas.NewText("›", colFgDim)
	chevron.TextSize = 20
	spin := container.New(&fixedSize{18, 18}, r.spinner)
	right := container.NewHBox(
		container.NewCenter(spin),
		container.NewVBox(layoutSpacer(18), r.latText),
		layoutSpacer(6),
		container.NewVBox(layoutSpacer(15), chevron),
		layoutSpacer(4),
	)
	inner := container.NewBorder(nil, nil, iconBox, right, textCol)
	stack := container.NewStack(r.bg, container.NewPadded(inner))
	return widget.NewSimpleRenderer(stack)
}

func (r *serverRow) Tapped(_ *fyne.PointEvent) {
	if r.onTap != nil {
		r.onTap()
	}
}
func (r *serverRow) TappedSecondary(e *fyne.PointEvent) {
	if r.onSecondary != nil {
		r.onSecondary(e)
	}
}

func (r *serverRow) setSelected(b bool)  { r.selected = b; r.applyStyle() }
func (r *serverRow) setConnected(b bool) { r.connected = b; r.applyStyle() }
func (r *serverRow) applyStyle() {
	switch {
	case r.connected:
		r.bg.FillColor = colConnBG
	case r.selected:
		r.bg.FillColor = colSelBG
	default:
		r.bg.FillColor = color.Transparent
	}
	r.bg.Refresh()
}

func (r *serverRow) setLatency(s string) {
	if s == "..." {
		r.latText.Hide()
		r.spinner.Show()
		r.spinner.Start()
		return
	}
	r.spinner.Stop()
	r.spinner.Hide()
	r.latText.Show()
	r.latText.Text = s
	switch {
	case s == "timeout":
		r.latText.Color = colRed
	case s == "—" || s == "n/a":
		r.latText.Color = colFgDim
	default:
		r.latText.Color = colGreen
	}
	r.latText.Refresh()
}

// ============ helpers ============

func layoutSpacer(h float32) fyne.CanvasObject {
	rect := canvas.NewRectangle(color.Transparent)
	rect.SetMinSize(fyne.NewSize(1, h))
	return rect
}
