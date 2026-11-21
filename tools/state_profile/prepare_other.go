//go:build !windows

package main

import "context"

func runPlatformProfile(context.Context, []string) (bool, int) { return false, 0 }
