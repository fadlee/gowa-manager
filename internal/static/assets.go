package static

import (
	"embed"
	"io/fs"
)

//go:embed all:web
var embedded embed.FS

func FS() fs.FS {
	sub, err := fs.Sub(embedded, "web")
	if err != nil {
		return embedded
	}
	return sub
}
