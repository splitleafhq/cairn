// Command cairn is an issue tracker for coding agents first and people second.
package main

import (
	"fmt"
	"io"
	"os"
	"runtime/debug"

	"github.com/splitleafhq/cairn/internal/cli"
)

// version is the main module's version as Go recorded it in the binary: the tag for go install …@v0.1.0 or a build
// in a tagged checkout, a pseudo-version for other commits, and "(devel)" when Go recorded none.
func version() string {
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" {
		return bi.Main.Version
	}
	return "(devel)"
}

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	dir, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(stderr, "cairn:", err)
		return cli.ExitError
	}
	return cli.Run(args, cli.Env{Version: version(), Dir: dir, Stdout: stdout, Stderr: stderr, Getenv: os.Getenv})
}
