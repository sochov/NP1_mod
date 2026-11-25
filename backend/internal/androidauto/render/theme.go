package render

import "image/color"

// Palette follows the NCC colours (neutral grey, cobalt accent) used by the
// Web UI and the Android app.
type Palette struct {
	Bg, Paper, Line, Text, Muted, Faint        color.RGBA
	Accent, Soft, Danger, DangerSoft           color.RGBA
	OK, Warn                                   color.RGBA
	Glass                                      color.RGBA // overlay chip background (opaque base)
	PlateHit, PlateHitSoft                     color.RGBA
	PlateHitLine, PlateHitBorder, DangerBorder color.RGBA
	PlateBg, PlateText, PlateBorder            color.RGBA
	Dark                                       bool
}

var lightPalette = Palette{
	Bg: rgba(0xF2F3F6), Paper: rgba(0xFFFFFF), Line: rgba(0xE4E7EC), Text: rgba(0x1C2230), Muted: rgba(0x636C7C), Faint: rgba(0x98A1B0),
	Accent: rgba(0x2F5BD8), Soft: rgba(0xEBF0FD), Danger: rgba(0xB42318), DangerSoft: rgba(0xFDECEA),
	OK: rgba(0x16794F), Warn: rgba(0x8A5B10), Glass: rgba(0xFFFFFF),
	PlateHit: rgba(0xE8590C), PlateHitSoft: rgba(0xFFF1E8), PlateHitLine: rgba(0x8F2D14), PlateHitBorder: rgba(0xF3C4A6), DangerBorder: rgba(0xF4B8B2),
	PlateBg: rgba(0xFBFBF8), PlateText: rgba(0x1F3A2C), PlateBorder: rgba(0x3D5A46),
}

var darkPalette = Palette{
	Bg: rgba(0x0F1115), Paper: rgba(0x191C23), Line: rgba(0x2A2F38), Text: rgba(0xE8EBF0), Muted: rgba(0xA0A9B8), Faint: rgba(0x6E7787),
	Accent: rgba(0x8CABFF), Soft: rgba(0x212B45), Danger: rgba(0xF09991), DangerSoft: rgba(0x3A2220),
	OK: rgba(0x6FCF9F), Warn: rgba(0xE5C98C), Glass: rgba(0x191C23),
	PlateHit: rgba(0xFF8A4C), PlateHitSoft: rgba(0x3D2A20), PlateHitLine: rgba(0xFFD9C2), PlateHitBorder: rgba(0x6D4330), DangerBorder: rgba(0x6A3430),
	PlateBg: rgba(0xE9EDE5), PlateText: rgba(0x1F3A2C), PlateBorder: rgba(0x3D5A46),
	Dark: true,
}

// PaletteFor returns the light or dark palette.
func PaletteFor(dark bool) *Palette {
	if dark {
		return &darkPalette
	}
	return &lightPalette
}
