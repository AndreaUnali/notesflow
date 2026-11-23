package terminal

import (
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestGraphicMovesWithoutRetransmittingAndClearsBeforeLeaving(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stream")
	out, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	surface := New(out, true)
	bitmap, err := Image(image.NewRGBA(image.Rect(0, 0, 4, 6)))
	if err != nil {
		t.Fatal(err)
	}
	p := Placement{Graphic: bitmap, X: 8, Y: 3, Cols: 4, Rows: 3}
	write := func(frame string) {
		t.Helper()
		if n, err := surface.Write([]byte(frame)); err != nil || n != len(frame) {
			t.Fatalf("Write: %d, %v", n, err)
		}
	}
	write(ansi.Truncate(surface.Marker([]Placement{p})+"0123456789", 6, ""))
	p.Y = 1
	write(surface.Marker([]Placement{p}) + "next")
	write(leaveAlt)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	output := string(data)
	if strings.Contains(output, markerPrefix) || !strings.HasPrefix(output, "012345") {
		t.Fatal("the internal frame marker leaked or was truncated")
	}
	if strings.Count(output, "a=t,f=100") != 1 || strings.Count(output, "a=p") != 2 ||
		!strings.Contains(output, "\x1b[4;9H") || !strings.Contains(output, "\x1b[2;9H") {
		t.Fatalf("unexpected upload or placements: %q", output)
	}
	if i, j := strings.Index(output, "a=d,d=I"), strings.Index(output, leaveAlt); i < 0 || i >= j {
		t.Fatal("image must be deleted before leaving the alternate screen")
	}
}
