//go:build mce4d_explorer_assets

package main

import (
	"embed"
	"io/fs"
)

//go:embed explorer_dist
var explorerAssets embed.FS

func bundledExplorerAssets() fs.FS {
	files, err := fs.Sub(explorerAssets, "explorer_dist")
	if err != nil {
		return nil
	}
	return files
}
