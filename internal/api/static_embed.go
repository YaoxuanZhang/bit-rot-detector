package api

import (
	"embed"
	"io/fs"
)

//go:embed static
var rawStaticFS embed.FS

// staticFS is the sub-FS rooted at "static/" so that "/" maps to "index.html".
// fs.Sub on an embedded FS whose root directory is named "static" cannot fail.
var staticFS = func() fs.FS {
	sub, err := fs.Sub(rawStaticFS, "static")
	if err != nil {
		panic("api: failed to create static sub-FS: " + err.Error())
	}
	return sub
}()
