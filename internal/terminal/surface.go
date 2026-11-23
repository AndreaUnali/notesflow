// Package terminal places Kitty graphics alongside the Bubble Tea text renderer.
package terminal

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"image"
	"image/png"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
)

const (
	markerPrefix = "\x1b_notesflow:"
	terminator   = "\x1b\\"
	leaveAlt     = "\x1b[?1049l"
)

type Graphic struct {
	data          []byte
	Width, Height int
}

func PNG(data []byte) (*Graphic, error) {
	cfg, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	return &Graphic{data: data, Width: cfg.Width, Height: cfg.Height}, nil
}

func Image(img image.Image) (*Graphic, error) {
	var out bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := enc.Encode(&out, img); err != nil {
		return nil, err
	}
	return PNG(out.Bytes())
}

type Placement struct {
	Graphic          *Graphic
	X, Y, Cols, Rows int
}

type activeImage struct {
	Placement
	id uint32
}

type Surface struct {
	*os.File
	enabled bool
	mu      sync.Mutex
	serial  uint64
	latest  []Placement
	pending map[uint64][]Placement
	active  []activeImage
	nextID  uint32
}

func New(file *os.File, graphics bool) *Surface {
	var seed [4]byte
	_, _ = rand.Read(seed[:])
	return &Surface{File: file, enabled: graphics, pending: make(map[uint64][]Placement), nextID: binary.LittleEndian.Uint32(seed[:])}
}

func Enabled() bool {
	switch strings.ToLower(os.Getenv("NOTESFLOW_GRAPHICS")) {
	case "off", "0", "text":
		return false
	case "on", "1", "kitty":
		return true
	}
	if os.Getenv("TMUX") != "" || os.Getenv("STY") != "" {
		return false
	}
	switch strings.ToLower(os.Getenv("TERM")) {
	case "xterm-ghostty", "xterm-kitty":
		return true
	}
	return strings.EqualFold(os.Getenv("TERM_PROGRAM"), "ghostty") || os.Getenv("KITTY_WINDOW_ID") != ""
}

func (s *Surface) Enabled() bool { return s != nil && s.enabled }

func (s *Surface) CellRatio(cols, rows int) float64 {
	if s != nil {
		return cellRatio(s.File.Fd(), cols, rows)
	}
	return .5
}

// Marker goes at the start of the first text line. Write consumes it under
// the same output lock as the text frame so placements never lead it.
func (s *Surface) Marker(placements []Placement) string {
	if !s.Enabled() {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !slices.Equal(s.latest, placements) {
		s.serial++
		s.latest = slices.Clone(placements)
		s.pending[s.serial] = s.latest
		if s.serial > 8 {
			delete(s.pending, s.serial-8)
		}
	}
	return markerPrefix + strconv.FormatUint(s.serial, 10) + terminator
}

func (s *Surface) Write(input []byte) (int, error) {
	if !s.enabled {
		return s.File.Write(input)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	frame := input
	var graphics bytes.Buffer
	var next []Placement
	update := false
	if i := bytes.Index(frame, []byte(markerPrefix)); i >= 0 {
		start := i + len(markerPrefix)
		if end := bytes.Index(frame[start:], []byte(terminator)); end >= 0 {
			if serial, err := strconv.ParseUint(string(frame[start:start+end]), 10, 64); err == nil {
				next, update = s.pending[serial]
				update = update || serial == 0
			}
			frame = append(bytes.Clone(frame[:i]), frame[start+end+len(terminator):]...)
		}
	}
	if bytes.Contains(frame, []byte(leaveAlt)) {
		s.sync(&graphics, nil)
		if _, err := s.File.Write(graphics.Bytes()); err != nil {
			return 0, err
		}
		update = false
	}
	if update {
		graphics.Reset()
		s.sync(&graphics, next)
		if graphics.Len() != 0 {
			frame = append(bytes.Clone(frame), graphics.Bytes()...)
		}
	}
	if _, err := s.File.Write(frame); err != nil {
		return 0, err
	}
	return len(input), nil
}

func (s *Surface) sync(out *bytes.Buffer, destinations []Placement) {
	prior := s.active
	for _, img := range prior {
		if !slices.ContainsFunc(destinations, func(p Placement) bool { return p.Graphic == img.Graphic && p.Rows > 0 && p.Cols > 0 }) {
			fmt.Fprintf(out, "\x1b_Ga=d,d=I,i=%d,q=2;%s", img.id, terminator)
		}
	}
	s.active = make([]activeImage, 0, len(destinations))
	for _, d := range destinations {
		if d.Graphic == nil || d.Rows < 1 || d.Cols < 1 {
			continue
		}
		i := slices.IndexFunc(prior, func(a activeImage) bool { return a.Graphic == d.Graphic })
		if i >= 0 {
			img := prior[i]
			prior = slices.Delete(prior, i, i+1)
			if img.Placement != d {
				place(out, img.id, d)
			}
			img.Placement = d
			s.active = append(s.active, img)
		} else {
			s.nextID++
			if s.nextID == 0 {
				s.nextID++
			}
			transmit(out, s.nextID, d.Graphic.data)
			place(out, s.nextID, d)
			s.active = append(s.active, activeImage{d, s.nextID})
		}
	}
}

func transmit(out *bytes.Buffer, id uint32, png []byte) {
	data := base64.StdEncoding.EncodeToString(png)
	for offset := 0; offset < len(data); offset += 4096 {
		end := min(offset+4096, len(data))
		more := 0
		if end < len(data) {
			more = 1
		}
		if offset == 0 {
			fmt.Fprintf(out, "\x1b_Ga=t,f=100,t=d,i=%d,q=2,m=%d;", id, more)
		} else {
			fmt.Fprintf(out, "\x1b_Gm=%d;", more)
		}
		out.WriteString(data[offset:end])
		out.WriteString(terminator)
	}
}

func place(out *bytes.Buffer, id uint32, p Placement) {
	fmt.Fprintf(out, "\x1b7\x1b[%d;%dH\x1b_Ga=p,i=%d,p=1,q=2,C=1,z=1,c=%d,r=%d;%s\x1b8", p.Y+1, p.X+1, id, p.Cols, p.Rows, terminator)
}
