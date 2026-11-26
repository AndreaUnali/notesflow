package workflow

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/gofrs/flock"
)

var errInvalidPDF = errors.New("la compilazione non ha prodotto un PDF valido")

func prepareBuildDir(root, location string) (string, func(), error) {
	location = filepath.Clean(location)
	if !filepath.IsLocal(location) || location == "." {
		return "", nil, errors.New("scegli una cartella di compilazione interna al progetto")
	}
	dir := root
	for _, part := range strings.Split(location, string(os.PathSeparator)) {
		dir = filepath.Join(dir, part)
		if err := os.Mkdir(dir, 0755); err != nil && !errors.Is(err, os.ErrExist) {
			return "", nil, err
		}
		info, err := os.Lstat(dir)
		if err != nil || !info.IsDir() {
			return "", nil, fmt.Errorf("la cartella di compilazione deve essere una directory reale: %s", dir)
		}
	}
	lock := flock.New(filepath.Join(dir, ".compile.lock"))
	ok, err := lock.TryLock()
	if err != nil || !ok {
		_ = lock.Close()
		if err == nil {
			err = errors.New("una compilazione è già in corso")
		}
		return "", nil, err
	}
	return dir, func() { _ = lock.Close() }, nil
}

func publishPDF(source, dest string) error {
	pdf, err := os.Open(source)
	if err != nil {
		return err
	}
	defer pdf.Close()
	var header [5]byte
	if _, err := io.ReadFull(pdf, header[:]); err != nil || string(header[:]) != "%PDF-" {
		return errInvalidPDF
	}
	if _, err := pdf.Seek(0, io.SeekStart); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(dest), ".notesflow-pdf-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	_, copyErr := io.Copy(temp, pdf)
	if err := errors.Join(copyErr, temp.Close()); err != nil {
		return err
	}
	return os.Rename(temp.Name(), dest)
}
