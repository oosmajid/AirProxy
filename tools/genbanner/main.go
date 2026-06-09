package main

import (
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
	xdraw "golang.org/x/image/draw"
)

// genbanner <icon.png> <font-bold.ttf> <out.png>
func main() {
	iconPath, fontPath, outPath := os.Args[1], os.Args[2], os.Args[3]

	W, H := 1280, 420
	img := image.NewRGBA(image.Rect(0, 0, W, H))

	// گرادینت قطری آبی → فیروزه‌ای
	top := color.RGBA{37, 99, 235, 255}
	bot := color.RGBA{14, 165, 233, 255}
	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			t := (float64(x)/float64(W)*0.5 + float64(y)/float64(H)*0.5)
			img.Set(x, y, lerp(top, bot, t))
		}
	}

	// آیکون
	if f, err := os.Open(iconPath); err == nil {
		if src, err := png.Decode(f); err == nil {
			d := 240
			dst := image.NewRGBA(image.Rect(0, 0, d, d))
			xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), xdraw.Over, nil)
			draw.Draw(img, image.Rect(90, (H-d)/2, 90+d, (H-d)/2+d), dst, image.Point{}, draw.Over)
		}
		f.Close()
	}

	// فونت
	fb, _ := os.ReadFile(fontPath)
	ft, _ := opentype.Parse(fb)
	title := newFace(ft, 104)
	sub := newFace(ft, 34)

	textX := 380
	white := image.NewUniform(color.RGBA{255, 255, 255, 255})
	faint := image.NewUniform(color.RGBA{226, 240, 255, 255})

	drawText(img, title, white, textX, 215, "AirProxy")
	drawText(img, sub, faint, textX+4, 270, "V2Ray · local SOCKS5 / HTTP proxy")
	drawText(img, sub, faint, textX+4, 314, "for macOS · only one port, not the whole system")

	out, _ := os.Create(outPath)
	defer out.Close()
	png.Encode(out, img)
}

func newFace(ft *opentype.Font, size float64) font.Face {
	f, _ := opentype.NewFace(ft, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingFull})
	return f
}

func drawText(dst draw.Image, face font.Face, src image.Image, x, y int, s string) {
	d := &font.Drawer{Dst: dst, Src: src, Face: face, Dot: fixed.P(x, y)}
	d.DrawString(s)
}

func lerp(a, b color.RGBA, t float64) color.RGBA {
	if t < 0 {
		t = 0
	}
	if t > 1 {
		t = 1
	}
	return color.RGBA{
		uint8(float64(a.R) + (float64(b.R)-float64(a.R))*t),
		uint8(float64(a.G) + (float64(b.G)-float64(a.G))*t),
		uint8(float64(a.B) + (float64(b.B)-float64(a.B))*t),
		255,
	}
}
