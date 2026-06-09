package main

import (
	_ "embed"
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

//go:embed assets/Vazirmatn-Regular.ttf
var fontRegular []byte

//go:embed assets/Vazirmatn-Bold.ttf
var fontBold []byte

var (
	resFontRegular = fyne.NewStaticResource("Vazirmatn-Regular.ttf", fontRegular)
	resFontBold    = fyne.NewStaticResource("Vazirmatn-Bold.ttf", fontBold)
)

// پالت روشن و نرم — الهام‌گرفته از Happ
var (
	colBG       = color.NRGBA{R: 0xEA, G: 0xED, B: 0xF2, A: 0xFF} // پس‌زمینهٔ خاکستری روشن
	colCard     = color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF} // کارت سفید
	colSelBG    = color.NRGBA{R: 0xF1, G: 0xEE, B: 0xFD, A: 0xFF} // ردیف انتخاب‌شده (بنفش روشن)
	colConnBG   = color.NRGBA{R: 0xE6, G: 0xF7, B: 0xEC, A: 0xFF} // ردیف متصل (سبز روشن)
	colAccent   = color.NRGBA{R: 0x6C, G: 0x5C, B: 0xE7, A: 0xFF} // بنفش
	colFg       = color.NRGBA{R: 0x21, G: 0x25, B: 0x2E, A: 0xFF} // متن اصلی تیره
	colFgDim    = color.NRGBA{R: 0xA2, G: 0xA8, B: 0xB6, A: 0xFF} // متن کم‌رنگ
	colGreen    = color.NRGBA{R: 0x2F, G: 0xB8, B: 0x5F, A: 0xFF}
	colRed      = color.NRGBA{R: 0xEC, G: 0x5B, B: 0x5B, A: 0xFF}
	colSeprator = color.NRGBA{R: 0xED, G: 0xEF, B: 0xF3, A: 0xFF}
	colRing     = color.NRGBA{R: 0xDF, G: 0xE3, B: 0xEB, A: 0xFF} // حلقهٔ دکمهٔ گرد
)

type appTheme struct{}

var _ fyne.Theme = (*appTheme)(nil)

func (appTheme) Color(n fyne.ThemeColorName, _ fyne.ThemeVariant) color.Color {
	switch n {
	case theme.ColorNameBackground:
		return colBG
	case theme.ColorNameForeground:
		return colFg
	case theme.ColorNameDisabled:
		return colFgDim
	case theme.ColorNamePlaceHolder:
		return colFgDim
	case theme.ColorNamePrimary:
		return colAccent
	case theme.ColorNameButton:
		return colCard
	case theme.ColorNameInputBackground:
		return colCard
	case theme.ColorNameSeparator:
		return colSeprator
	case theme.ColorNameHover:
		return color.NRGBA{0, 0, 0, 0x12} // پوشش تیرهٔ ملایم هنگام hover
	case theme.ColorNamePressed:
		return color.NRGBA{0, 0, 0, 0x1F}
	case theme.ColorNameMenuBackground, theme.ColorNameOverlayBackground:
		return colCard
	case theme.ColorNameSelection:
		return colSelBG
	}
	return theme.DefaultTheme().Color(n, theme.VariantLight)
}

func (appTheme) Font(s fyne.TextStyle) fyne.Resource {
	if s.Monospace {
		return theme.DefaultTheme().Font(s)
	}
	if s.Bold {
		return resFontBold
	}
	return resFontRegular
}

func (appTheme) Icon(n fyne.ThemeIconName) fyne.Resource { return theme.DefaultTheme().Icon(n) }

func (appTheme) Size(n fyne.ThemeSizeName) float32 {
	switch n {
	case theme.SizeNameText:
		return 13.5
	case theme.SizeNamePadding:
		return 6
	case theme.SizeNameInputRadius, theme.SizeNameSelectionRadius:
		return 10
	}
	return theme.DefaultTheme().Size(n)
}
