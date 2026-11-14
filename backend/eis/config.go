package eis

import (
	"os"
	"path/filepath"
)

func TmpDir() string    { return dirFor("TENDERSCOPE_TMP_DIR", "tmp") }
func UploadDir() string { return dirFor("TENDERSCOPE_UPLOAD_DIR", "uploads") }
func CertsDir() string  { return dirFor("TENDERSCOPE_CERTS_DIR", "certs") }

func dirFor(envName, defaultName string) string {
	if v := os.Getenv(envName); v != "" {
		if abs, err := filepath.Abs(v); err == nil {
			return abs
		}
		return v
	}
	return filepath.Join(backendRoot(), defaultName)
}

func backendRoot() string {
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	for d := wd; ; {
		if fileExists(filepath.Join(d, "go.mod")) {
			return d
		}
		if fileExists(filepath.Join(d, "backend", "go.mod")) {
			return filepath.Join(d, "backend")
		}
		parent := filepath.Dir(d)
		if parent == d {
			return wd
		}
		d = parent
	}
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
