package tui

import (
	"image"
	"image/color"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"

	"github.com/AndreaUnali/notesflow/internal/palette"
	"github.com/AndreaUnali/notesflow/internal/terminal"
)

// Use a locally installed font so Notesflow can keep terminal fonts and the wordmark independent.
var wordmarkFont = sync.OnceValue(func() *opentype.Font {
	var paths []string
	switch runtime.GOOS {
	case "darwin":
		paths = []string{
			"/System/Library/Fonts/Supplemental/Times New Roman.ttf",
			"/Library/Fonts/Times New Roman.ttf",
			"/System/Library/Fonts/Times.ttc",
		}
	case "windows":
		if windows := os.Getenv("WINDIR"); windows != "" {
			paths = []string{filepath.Join(windows, "Fonts", "times.ttf")}
		}
	default:
		paths = []string{
			"/usr/share/fonts/truetype/msttcorefonts/Times_New_Roman.ttf",
			"/usr/share/fonts/truetype/liberation2/LiberationSerif-Regular.ttf",
			"/usr/share/fonts/truetype/liberation/LiberationSerif-Regular.ttf",
		}
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		collection, err := opentype.ParseCollection(data)
		if err == nil {
			if serif, err := collection.Font(0); err == nil {
				return serif
			}
		}
	}
	return graphFont()
})

func launchWordmark(cols, rows int, cellRatio float64) *terminal.Graphic {
	return renderWordmark(cols, rows, cellRatio, palette.Cyan, palette.Purple)
}

func renderWordmark(cols, rows int, cellRatio float64, from, to string) *terminal.Graphic {
	markFont := wordmarkFont()
	if markFont == nil {
		return nil
	}
	w, h := max(1, int(float64(cols*22)*cellRatio)), rows*22
	const name = "Notesflow"
	var face font.Face
	for size := float64(h) * .80; size >= 9; size *= .86 {
		candidate, err := opentype.NewFace(markFont, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingNone})
		if err != nil {
			return nil
		}
		if font.MeasureString(candidate, name).Ceil() <= w-12 {
			face = candidate
			break
		}
		_ = candidate.Close()
	}
	if face == nil {
		return nil
	}
	defer face.Close()
	bounds, _ := font.BoundString(face, name)
	left := (w - (bounds.Max.X - bounds.Min.X).Ceil()) / 2
	top := (h - (bounds.Max.Y - bounds.Min.Y).Ceil()) / 2
	mask := image.NewAlpha(image.Rect(0, 0, w, h))
	drawer := font.Drawer{Dst: mask, Src: image.Opaque, Face: face,
		Dot: fixed.Point26_6{X: fixed.I(left) - bounds.Min.X, Y: fixed.I(top) - bounds.Min.Y}}
	drawer.DrawString(name)

	img := image.NewNRGBA(mask.Bounds())
	start, end := rgb(from), rgb(to)
	textWidth := max(1, (bounds.Max.X - bounds.Min.X).Ceil())
	colors := make([]color.NRGBA, w)
	for x := range colors {
		mix := max(0, min(1, float64(x-left)/float64(textWidth)))
		pick := func(a, b uint8) uint8 { return uint8(float64(a)*(1-mix) + float64(b)*mix) }
		colors[x] = color.NRGBA{R: pick(start.R, end.R), G: pick(start.G, end.G), B: pick(start.B, end.B), A: 255}
	}
	for y := range h {
		line := mask.Pix[y*mask.Stride:][:w]
		if !slices.ContainsFunc(line, func(a uint8) bool { return a != 0 }) {
			continue
		}
		for x, opacity := range line {
			if opacity > 0 {
				c := colors[x]
				c.A = opacity
				img.SetNRGBA(x, y, c)
			}
		}
	}
	graphic, _ := terminal.Image(img)
	return graphic
}
