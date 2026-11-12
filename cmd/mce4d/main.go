package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "mce4d: bootstrap only; product capabilities are not available")
	os.Exit(3)
}
