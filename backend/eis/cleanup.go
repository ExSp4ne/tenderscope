package eis

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// CleanupOldFiles удаляет подпапки TmpDir() и файлы UploadDir(), которые не менялись дольше maxAge.
// Отсутствие папок не считается ошибкой; ошибки по отдельным файлам собираются и возвращаются вместе.
func CleanupOldFiles(maxAge time.Duration) error {
	now := time.Now()
	return errors.Join(
		cleanDir(TmpDir(), maxAge, now, true),
		cleanDir(UploadDir(), maxAge, now, false),
	)
}

// cleanDir удаляет в dir только подпапки (dirsOnly) или только обычные файлы старше maxAge.
// Симлинки не трогаем.
func cleanDir(dir string, maxAge time.Duration, now time.Time, dirsOnly bool) error {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var errs []error
	for _, e := range entries {
		if e.IsDir() != dirsOnly || e.Type()&fs.ModeSymlink != 0 {
			continue
		}
		info, err := e.Info()
		if err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				errs = append(errs, err)
			}
			continue
		}
		if now.Sub(info.ModTime()) <= maxAge {
			continue
		}
		if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
