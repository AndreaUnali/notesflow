package workflow

import (
	"context"
	"fmt"
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

func PDFPage(ctx context.Context, file string, page int) ([]byte, error) {
	renderer, err := exec.LookPath("pdftoppm")
	if err != nil {
		return nil, fmt.Errorf("installa Poppler per l’anteprima PDF")
	}
	p := strconv.Itoa(page)
	result, err := exec.CommandContext(ctx, renderer, "-f", p, "-l", p, "-scale-to", "1400", "-singlefile", "-png", file).Output()
	if err != nil {
		return nil, fmt.Errorf("impossibile leggere la pagina %d del PDF", page)
	}
	return result, nil
}

func PDFText(ctx context.Context, file string, page int) (string, error) {
	extractor, err := exec.LookPath("pdftotext")
	if err != nil {
		return "", fmt.Errorf("installa Poppler per leggere il PDF")
	}
	p := strconv.Itoa(page)
	result, err := exec.CommandContext(ctx, extractor, "-f", p, "-l", p, "-nopgbrk", file, "-").Output()
	if err != nil {
		return "", fmt.Errorf("impossibile estrarre il testo della pagina %d", page)
	}
	return strings.TrimSpace(string(result)), nil
}
