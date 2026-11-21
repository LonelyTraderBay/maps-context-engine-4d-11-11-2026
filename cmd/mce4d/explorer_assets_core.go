//go:build !mce4d_explorer_assets

package main

import "io/fs"

func bundledExplorerAssets() fs.FS { return nil }
