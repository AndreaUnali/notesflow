package workflow

import (
	"context"
	"fmt"
	"io"
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

// PDFTextPages extracts a contiguous range in one pass, retaining blank pages.
func PDFTextPages(ctx context.Context, file string, first, last int) ([]string, error) {
	extractor, err := exec.LookPath("pdftotext")
	if err != nil {
		return nil, fmt.Errorf("installa Poppler per leggere il PDF")
	}
	cmd := exec.CommandContext(ctx, extractor, "-f", strconv.Itoa(first), "-l", strconv.Itoa(last), file, "-")
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	const maxExtracted = 8 << 20
	data, readErr := io.ReadAll(io.LimitReader(out, maxExtracted+1))
	if readErr != nil || len(data) > maxExtracted {
		_ = cmd.Process.Kill()
	}
	waitErr := cmd.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(data) > maxExtracted {
		return nil, fmt.Errorf("il testo estratto dal PDF supera 8 MB")
	}
	pages := strings.Split(strings.TrimSuffix(string(data), "\f"), "\f")
	if readErr != nil || waitErr != nil || len(pages) != last-first+1 {
		return nil, fmt.Errorf("impossibile estrarre il testo delle pagine %d–%d", first, last)
	}
	return pages, nil
}
