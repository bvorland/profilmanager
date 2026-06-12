// Package main is the entry point for the profilmanager (pm) binary.
//
// All real logic lives in internal/* packages; main is intentionally trivial
// so the binary stays a thin shell around internal/cli.Execute.
package main

import (
	"os"

	"github.com/bvorland/profilmanager/internal/cli"
)

func main() {
	err := cli.Execute()
	os.Exit(cli.CodeFor(err))
}
