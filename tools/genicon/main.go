package main

import (
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
)

const SS = 2 // supersample

func main() {
	size := 1024
	hi := size * SS
	img := image.NewRGBA(image.Rect(0, 0, hi, hi))

	cx, cy := float64(hi)/2, float64(hi)/2

	// پس‌زمینهٔ گرادینت با گوشه‌های گرد
	radius := float64(hi) * 0.235
	top := color.RGBA{37, 99, 235, 255}   // #2563EB
	bot := color.RGBA{14, 165, 233, 255}  // #0EA5E9
	for y := 0; y < hi; y++ {
		t := float64(y) / float64(hi)
		bg := lerp(top, bot, t)
		for x := 0; x < hi; x++ {
			if !inRoundRect(float64(x), float64(y), float64(hi), radius) {
				continue
			}
			img.Set(x, y, bg)
		}
	}

	white := color.RGBA{255, 255, 255, 255}
	R := float64(hi) * 0.30 // شعاع گلوب
	stroke := float64(hi) * 0.018

	for y := 0; y < hi; y++ {
		for x := 0; x < hi; x++ {
			dx, dy := float64(x)-cx, float64(y)-cy
			d := math.Hypot(dx, dy)
			if d > R+stroke {
				continue
			}
			on := false
			// محیط دایره
			if math.Abs(d-R) <= stroke {
				on = true
			}
			// مدار وسط عمودی و افقی + دو نصف‌النهار + دو مدار
			if d <= R {
				if ellipseStroke(dx, dy, R*0.45, R, stroke) {
					on = true
				}
				if ellipseStroke(dx, dy, R, R*0.42, stroke) {
					on = true
				}
				if math.Abs(dx) <= stroke*0.7 { // نصف‌النهار مرکزی
					on = true
				}
				if math.Abs(dy) <= stroke*0.7 { // استوا
					on = true
				}
			}
			if on {
				img.Set(x, y, white)
			}
		}
	}

	// downsample SSxSS با میانگین‌گیری برای ضدّ-دندانه‌دار شدن
	out := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			var r, g, b, a int
			for sy := 0; sy < SS; sy++ {
				for sx := 0; sx < SS; sx++ {
					c := img.RGBAAt(x*SS+sx, y*SS+sy)
					r += int(c.R)
					g += int(c.G)
					b += int(c.B)
					a += int(c.A)
				}
			}
			n := SS * SS
			out.SetRGBA(x, y, color.RGBA{uint8(r / n), uint8(g / n), uint8(b / n), uint8(a / n)})
		}
	}

	f, _ := os.Create(os.Args[1])
	defer f.Close()
	png.Encode(f, out)
}

func ellipseStroke(dx, dy, a, b, t float64) bool {
	// مقدار نرمال‌شده؛ نزدیک ۱ یعنی روی محیط بیضی
	v := math.Hypot(dx/a, dy/b)
	// تبدیل به فاصلهٔ تقریبی بر حسب پیکسل
	grad := math.Hypot(dx/(a*a), dy/(b*b))
	if grad == 0 {
		return false
	}
	dist := math.Abs(v-1) / grad
	return dist <= t
}

func inRoundRect(px, py, size, r float64) bool {
	// گوشه‌های گرد
	x := px
	y := py
	if x < r && y < r {
		return math.Hypot(r-x, r-y) <= r
	}
	if x > size-r && y < r {
		return math.Hypot(x-(size-r), r-y) <= r
	}
	if x < r && y > size-r {
		return math.Hypot(r-x, y-(size-r)) <= r
	}
	if x > size-r && y > size-r {
		return math.Hypot(x-(size-r), y-(size-r)) <= r
	}
	return true
}

func lerp(a, b color.RGBA, t float64) color.RGBA {
	return color.RGBA{
		uint8(float64(a.R) + (float64(b.R)-float64(a.R))*t),
		uint8(float64(a.G) + (float64(b.G)-float64(a.G))*t),
		uint8(float64(a.B) + (float64(b.B)-float64(a.B))*t),
		255,
	}
}
