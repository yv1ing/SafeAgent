//go:build !embedded

package web

import (
	"io/fs"
	"os"
)

func Assets() (fs.FS, error) { return os.DirFS("web/dist"), nil }
