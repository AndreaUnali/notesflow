package tui

import (
	"fmt"
	"image"
	"image/draw"
	"math"
	"time"

	"github.com/AndreaUnali/notesflow/internal/agent"
	"github.com/AndreaUnali/notesflow/internal/palette"
	"github.com/AndreaUnali/notesflow/internal/terminal"
)

type agentNotice struct {
	id       int
	title    string
	text     string
	question bool
	until    time.Time
	seconds  int
	icon     *terminal.Graphic
}

func (m *Model) toastAgent(id int, title, text string, question bool) {
	notice := agentNotice{id: id, title: title, text: text, question: question, seconds: -1}
	if !question {
		notice.until = time.Now().Add(6 * time.Second)
	}
	if m.agents.notice.question && m.agents.notice.id != id && !question {
		m.agents.queued = append(m.agents.queued, notice)
		return
	}
	m.agents.notice = notice
}

func (m *Model) renderNotice(now time.Time) {
	notice := &m.agents.notice
	if !notice.question || m.agents.book == nil {
		return
	}
	job, ok := m.agents.book.Lookup(notice.id)
	if !ok || job.State != agent.Waiting || job.Deadline.IsZero() {
		m.agents.notice = agentNotice{}
		return
	}
	left := job.Deadline.Sub(now)
	seconds := max(0, int(math.Ceil(left.Seconds())))
	if notice.seconds == seconds && (notice.icon != nil || !m.surface.Enabled()) {
		return
	}
	notice.seconds = seconds
	if m.surface.Enabled() {
		notice.icon = ringImage(1-float64(left)/float64(m.questionDuration()), seconds)
	}
}

func (m Model) noticeRect() cellRect {
	if m.agents.notice.id == 0 || m.width < 30 || m.height < 12 {
		return cellRect{}
	}
	width := min(62, max(26, m.width*45/100))
	return cellRect{m.width - width, 0, width, 4}
}

func (m Model) overlayNotice(lines []string, images []terminal.Placement) ([]string, []terminal.Placement) {
	r := m.noticeRect()
	if r.cols == 0 || len(lines) < r.rows {
		return lines, images
	}
	notice := m.agents.notice
	boxWidth := r.cols
	body := clean(notice.text)
	if notice.question {
		if notice.icon != nil && boxWidth >= 33 {
			ringWidth := max(6, int(math.Round(4/m.surface.CellRatio(m.width, m.height))))
			boxWidth -= ringWidth
			images = append(images, terminal.Placement{Graphic: notice.icon, X: r.x + boxWidth, Y: r.y, Cols: ringWidth, Rows: r.rows})
		}
		seconds := max(0, notice.seconds)
		notice.title += fmt.Sprintf(" · %02d:%02d", seconds/60, seconds%60)
		if notice.icon == nil {
			marks := []string{"◔", "◑", "◕"}
			stage := min(2, max(0, int(3*(1-float64(seconds)/m.questionDuration().Seconds()))))
			notice.title = marks[stage] + " " + notice.title
		}
	}
	wrapped := foldLine(body, boxWidth-4)
	for i := range wrapped {
		wrapped[i] = " " + wrapped[i]
	}
	rendered := panel(notice.title, wrapped, boxWidth, r.rows, true)
	for i, value := range rendered {
		lines[r.y+i] = overlayLine(lines[r.y+i], r.x, r.cols, value)
	}
	return lines, images
}

func ringImage(progress float64, seconds int) *terminal.Graphic {
	const size = 72
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	draw.Draw(img, img.Bounds(), image.NewUniform(surfaceRGB), image.Point{}, draw.Src)
	center := float64(size) / 2
	at := func(angle, radius float64) point {
		return point{center + radius*math.Sin(angle), center - radius*math.Cos(angle)}
	}
	ring := courseRGB[0]
	switch {
	case seconds <= 15:
		ring = courseRGB[1]
	case seconds <= 45:
		ring = courseRGB[3]
	}
	outline := rgb(palette.Line)
	for i := range 224 {
		fraction := float64(i) / 224
		shade := outline
		if fraction <= progress {
			shade = ring
		}
		dot(img, at(fraction*2*math.Pi, 29), 1.65, shade, .9)
	}
	var points [5]point
	for i := range points {
		points[i] = at(float64(i)*2*math.Pi/5, 13)
	}
	for i, p := range points {
		for _, neighbor := range points[i+1:] {
			line(img, p, neighbor, courseRGB[0], .7, .65, false)
		}
		dot(img, p, 1.8, courseRGB[0], 1)
	}
	graphic, _ := terminal.Image(img)
	return graphic
}
