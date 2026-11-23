package tui

import (
	"image"
	"image/color"
	"image/draw"
	"maps"
	"math"
	"slices"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"

	"github.com/AndreaUnali/notesflow/internal/parser"
	"github.com/AndreaUnali/notesflow/internal/terminal"
)

type cellRect struct{ x, y, cols, rows int }

func (r cellRect) contains(x, y int) bool {
	return x >= r.x && y >= r.y && x < r.x+r.cols && y < r.y+r.rows
}

type graphRenderKey struct {
	revision               uint64
	scope, selected, hover string
	cols, rows, filter     int
	ratio, zoom            float64
	pan                    point
	dragVersion            uint64
}

type renderedGraph struct {
	key           graphRenderKey
	graphic       *terminal.Graphic
	points        map[string]point
	width, height int
	scale         float64
}

func (m Model) graphRect() cellRect {
	layout := m.layout()
	return cellRect{1, layout.header + 2, layout.left - 2, layout.body - 6}
}

func edgeKind(kind string) int {
	switch kind {
	case "semantic":
		return 2
	case "hierarchy":
		return 3
	default:
		return 1
	}
}

func (m *Model) prepareGraphic() {
	if !m.surface.Enabled() || m.tab != graphTab {
		m.graphSurface = renderedGraph{}
		return
	}
	node, ok := m.graphNode()
	box := m.graphRect()
	if !ok || box.cols < 20 || box.rows < 5 {
		m.graphSurface = renderedGraph{}
		return
	}
	key := graphRenderKey{
		revision: m.canvas.revision, scope: m.canvas.scope, selected: node.ID, hover: m.graphHover,
		cols: box.cols, rows: box.rows, filter: m.edgeFilter, ratio: m.surface.CellRatio(m.width, m.height),
		zoom: m.zoom, pan: m.pan, dragVersion: m.graphDragVersion,
	}
	if key != m.graphSurface.key {
		m.graphSurface = renderGraph(m.canvas, key)
	}
}

func (m Model) graphHit(x, y int) string {
	r := m.graphRect()
	g := m.graphSurface
	if g.graphic == nil || !r.contains(x, y) {
		return ""
	}
	cx := float64(g.width) / float64(r.cols)
	cy := float64(g.height) / float64(r.rows)
	px, py := (float64(x-r.x)+.5)*cx, (float64(y-r.y)+.5)*cy
	best := max(14, cy*.8)
	match := ""
	for id, p := range g.points {
		if distance := math.Hypot(px-p.x, py-p.y); distance < best {
			match, best = id, distance
		}
	}
	return match
}

func (m *Model) moveGraphNode(id string, dx, dy int) {
	g := m.graphSurface
	r := m.graphRect()
	if id == "" || g.scale <= 0 || r.cols < 1 || r.rows < 1 {
		return
	}
	p := m.canvas.positions[id]
	p.x += float64(dx*g.width) / float64(r.cols) / g.scale
	p.y += float64(dy*g.height) / float64(r.rows) / g.scale
	m.canvas.positions = maps.Clone(m.canvas.positions)
	m.canvas.positions[id] = p
	m.graphDragVersion++
}

var graphFont = sync.OnceValue(func() *opentype.Font {
	f, _ := opentype.Parse(goregular.TTF)
	return f
})

var (
	surfaceRGB = hexColor(0x090B10)
	courseRGB  = []color.RGBA{hexColor(0x00F5D4), hexColor(0xFF2A85), hexColor(0x9B5DE5), hexColor(0xFEE440)}
)

func hexColor(value uint32) color.RGBA {
	return color.RGBA{R: uint8(value >> 16), G: uint8(value >> 8), B: uint8(value), A: 255}
}

func renderGraph(g terminalGraph, key graphRenderKey) renderedGraph {
	// Render at pixel resolution, independently of the terminal character grid.
	pixelsPerRow := max(7, min(22, int(min(850/float64(key.rows), 1200/(float64(key.cols)*key.ratio)))))
	w, h := max(1, int(float64(key.cols*pixelsPerRow)*key.ratio)), key.rows*pixelsPerRow
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), image.NewUniform(surfaceRGB), image.Point{}, draw.Src)
	spacing := max(17, pixelsPerRow+4)
	for y := spacing / 2; y < h; y += spacing {
		for x := spacing / 2; x < w; x += spacing {
			img.SetRGBA(x, y, hexColor(0x233047))
		}
	}
	points := make(map[string]point, len(g.positions))
	left, right, top, bottom := g.bounds.left, g.bounds.right, g.bounds.top, g.bounds.bottom
	margin := min(float64(w)*.18, 112)
	fitX, fitY := max(1.3, right-left), max(1.1, bottom-top)
	scale := max(1, min((float64(w)-2*margin)/fitX, (float64(h)-36)/fitY)) * max(.3, key.zoom)
	for id, p := range g.positions {
		points[id] = point{float64(w)/2 + (p.x-(left+right)/2)*scale + key.pan.x*float64(w)/float64(key.cols),
			float64(h)/2 + (p.y-(top+bottom)/2)*scale + key.pan.y*float64(h)/float64(key.rows)}
	}
	focus := key.hover
	neighbors := make(map[string]bool)
	for _, edge := range g.edges {
		kind := edgeKind(edge.Kind)
		if key.filter != 0 && key.filter != kind {
			continue
		}
		a, b := points[edge.Source], points[edge.Target]
		strength := .64
		if focus != "" {
			if edge.Source == focus || edge.Target == focus {
				neighbors[edge.Source], neighbors[edge.Target] = true, true
				strength = 1
			} else {
				strength = .14
			}
		} else if edge.Source == key.selected || edge.Target == key.selected {
			strength = .96
		}
		shade, stroke, dashed := hexColor(0x60ECDA), 1.05, false
		switch kind {
		case 2:
			shade, stroke, dashed = hexColor(0xFF6FB7), .8+1.25*edge.Weight, true
		case 3:
			shade, stroke = hexColor(0x71839C), .70
		}
		line(img, a, b, shade, stroke, strength, dashed)
	}
	order := slices.Clone(g.nodes)
	courses := map[string]int{}
	for _, node := range order {
		if _, exists := courses[node.Course]; !exists {
			courses[node.Course] = len(courses)
		}
	}
	slices.SortStableFunc(order, func(a, b parser.Node) int {
		rank := func(id string) int {
			if id == focus {
				return 4
			}
			if id == key.selected {
				return 3
			}
			if neighbors[id] {
				return 2
			}
			return 1
		}
		return rank(b.ID) - rank(a.ID)
	})
	type label struct {
		caption string
		at      fixed.Point26_6
		shade   color.RGBA
	}
	var labels []label
	taken := make([]image.Rectangle, 0, len(order)*2)
	labelSize := max(12, min(17, float64(pixelsPerRow)*.73))
	var face font.Face
	if f := graphFont(); f != nil {
		face, _ = opentype.NewFace(f, &opentype.FaceOptions{Size: labelSize, DPI: 72, Hinting: font.HintingFull})
	}
	if face != nil {
		defer face.Close()
	}
	for _, node := range g.nodes {
		p := points[node.ID]
		taken = append(taken, image.Rect(int(p.x)-8, int(p.y)-8, int(p.x)+8, int(p.y)+8))
	}
	for _, node := range order {
		p := points[node.ID]
		if p.x < 0 || p.y < 0 || p.x > float64(w) || p.y > float64(h) {
			continue
		}
		c := courseRGB[courses[node.Course]%len(courseRGB)]
		active := focus == "" || node.ID == focus || neighbors[node.ID]
		r := 4.2
		if node.Kind == "file" {
			r = 6.2
		}
		if node.ID == key.selected || node.ID == focus {
			glow(img, p, c, r+2, .95)
			dot(img, p, r+2.7, hexColor(0xECF9FF), .9)
			dot(img, p, r+1.4, surfaceRGB, 1)
		}
		alpha := .26
		if active {
			alpha = 1
			glow(img, p, c, r, .74)
		}
		dot(img, p, r, c, alpha)
		if face == nil || (!active && node.ID != key.selected) || (len(order) > 35 && key.zoom < 1.3 && node.Kind != "file" && node.ID != key.selected && node.ID != focus) {
			continue
		}
		caption := labelText(node.Title, 32)
		measure := font.MeasureString(face, caption).Ceil()
		x, y := int(p.x), int(p.y)
		candidates := []image.Point{{X: x + int(r) + 8, Y: y + 4}, {X: x - measure - int(r) - 8, Y: y + 4}, {X: x - measure/2, Y: y - int(r) - 8}, {X: x - measure/2, Y: y + int(r) + int(labelSize) + 4}}
		for _, at := range candidates {
			bounds := image.Rect(at.X-3, at.Y-int(labelSize)-2, at.X+measure+3, at.Y+5)
			if !bounds.In(img.Bounds()) || slices.ContainsFunc(taken, func(a image.Rectangle) bool { return a.Overlaps(bounds) }) {
				continue
			}
			taken = append(taken, bounds)
			shade := hexColor(0xBBC9DB)
			if node.ID == key.selected || node.ID == focus {
				shade = hexColor(0xF1FAFF)
			}
			labels = append(labels, label{caption, fixed.P(at.X, at.Y), shade})
			break
		}
	}
	for _, item := range labels {
		d := font.Drawer{Dst: img, Src: image.NewUniform(surfaceRGB), Face: face, Dot: item.at.Add(fixed.P(1, 1))}
		d.DrawString(item.caption)
		d.Src, d.Dot = image.NewUniform(item.shade), item.at
		d.DrawString(item.caption)
	}
	graphic, _ := terminal.Image(img)
	return renderedGraph{key: key, graphic: graphic, points: points, width: w, height: h, scale: scale}
}

func labelText(s string, limit int) string {
	r := []rune(clean(s))
	if len(r) > limit {
		return string(r[:limit-1]) + "…"
	}
	return string(r)
}

func line(img *image.RGBA, a, b point, c color.RGBA, radius, alpha float64, dashed bool) {
	length := math.Hypot(b.x-a.x, b.y-a.y)
	steps := max(1, int(length))
	for i := range steps {
		if dashed && i%15 >= 9 {
			continue
		}
		f := float64(i) / float64(steps)
		dot(img, point{a.x + (b.x-a.x)*f, a.y + (b.y-a.y)*f}, radius, c, alpha*.66)
	}
}

func glow(img *image.RGBA, p point, c color.RGBA, r, alpha float64) {
	for i := 4; i > 0; i-- {
		dot(img, p, r+float64(i)*2.3, c, alpha*.061)
	}
}

func dot(img *image.RGBA, p point, r float64, c color.RGBA, alpha float64) {
	b := img.Bounds()
	for y := max(0, int(p.y-r-1)); y < min(b.Max.Y, int(p.y+r+2)); y++ {
		for x := max(0, int(p.x-r-1)); x < min(b.Max.X, int(p.x+r+2)); x++ {
			d := math.Hypot(float64(x)-p.x, float64(y)-p.y)
			a := max(0, min(1, r+.65-d)) * alpha
			if a == 0 {
				continue
			}
			i := img.PixOffset(x, y)
			img.Pix[i] = uint8(float64(img.Pix[i])*(1-a) + float64(c.R)*a)
			img.Pix[i+1] = uint8(float64(img.Pix[i+1])*(1-a) + float64(c.G)*a)
			img.Pix[i+2] = uint8(float64(img.Pix[i+2])*(1-a) + float64(c.B)*a)
		}
	}
}
