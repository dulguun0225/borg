package clientdist

import (
	"embed"
	"io/fs"
)

// FS is browser/ as it was embedded at build time. The "all:" prefix is what
// lets the directory embed on a fresh clone, where the client's build has not
// run and browser/ holds nothing but .gitkeep.
//
//go:embed all:browser
var FS embed.FS

// Browser is FS rooted at browser/, the directory package screens' Server.New
// takes as its client argument.
func Browser() fs.FS {
	sub, err := fs.Sub(FS, "browser")
	if err != nil {
		// fs.Sub over a fixed, embedded path fails only if the embed
		// directive above stops matching this literal, which is a build-time
		// mistake and not a runtime one.
		panic(err)
	}
	return sub
}
