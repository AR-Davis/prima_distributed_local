// Package main is the entry point for the prima-installer
package main

import (
	"fmt"
	"os"

	"github.com/AR-Davis/prima_distributed_local/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}