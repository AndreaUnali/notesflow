package workflow

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"os/exec"
	"strconv"
	"strings"
)

// PDFPages reads the number of pages, if Poppler can inspect the file.
func PDFPages(ctx context.Context, file string) int {
	output, err := exec.CommandContext(ctx, "pdfinfo", file).Output()
	if err != nil {
		return 0
	}
	for _, row := range strings.Split(string(output), "\n") {
		if value, ok := strings.CutPrefix(row, "Pages:"); ok {
			pages, _ := strconv.Atoi(strings.TrimSpace(value))
			return pages
		}
	}
	return 0
}

// PDFPage rasterizes one page. The preview is bounded before PNG decoding.
func PDFPage(ctx context.Context, file string, page, size int) (image.Image, error) {
	renderer, err := exec.LookPath("pdftoppm")
	if err != nil {
		return nil, fmt.Errorf("serve pdftoppm (Poppler) per l’anteprima")
	}
	p := strconv.Itoa(page)
	result, err := exec.CommandContext(ctx, renderer, "-f", p, "-l", p, "-scale-to",
		strconv.Itoa(min(256, max(32, size))), "-singlefile", "-png", file).Output()
	if err != nil {
		return nil, fmt.Errorf("impossibile leggere la pagina %d del PDF", page)
	}
	return png.Decode(bytes.NewReader(result))
}
