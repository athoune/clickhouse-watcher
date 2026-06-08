package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type mode int

const (
	modeDaemon mode = iota
	modeClient
	modeStandalone
	modeUnknown
)

// resolveMode determines the run mode from os.Args[0] (binary name) and
// optional subcommand in os.Args[1].
//
// Supported invocations:
//   clickhouse-watch-serve  [...]     → daemon
//   clickhouse-watch-client [...]     → client
//   clickhouse-watch serve  [...]     → daemon
//   clickhouse-watch client [...]     → client
//   clickhouse-watch                  → standalone (daemon + client)
func resolveMode() mode {
	basename := filepath.Base(os.Args[0])

	// ARGV[0]-based detection (symlinks)
	if strings.HasPrefix(basename, "clickhouse-watch-") {
		suffix := basename[len("clickhouse-watch-"):]
		switch suffix {
		case "serve", "d":
			return modeDaemon
		case "client":
			return modeClient
		}
	}

	// Subcommand-based detection
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "serve":
			return modeDaemon
		case "client":
			return modeClient
		}
		// Unknown subcommand
		return modeUnknown
	}

	// No args, no matching basename → standalone
	return modeStandalone
}

func printUsage() {
	name := filepath.Base(os.Args[0])
	fmt.Fprintf(os.Stderr, "Usage: %s [serve|client]\n", name)
	fmt.Fprintf(os.Stderr, "\nModes:\n")
	fmt.Fprintf(os.Stderr, "  serve    Start the daemon server\n")
	fmt.Fprintf(os.Stderr, "  client   Start the TUI client\n")
	fmt.Fprintf(os.Stderr, "  (none)   Standalone mode (daemon + client in one process)\n")
	fmt.Fprintf(os.Stderr, "\nAlso works via symlink names:\n")
	fmt.Fprintf(os.Stderr, "  clickhouse-watch-serve  → daemon mode\n")
	fmt.Fprintf(os.Stderr, "  clickhouse-watch-client → client mode\n")
}

func main() {
	m := resolveMode()
	switch m {
	case modeDaemon:
		var args []string
		if len(os.Args) > 1 && os.Args[1] == "serve" {
			args = os.Args[2:]
		}
		runDaemon(args)
	case modeClient:
		runClient()
	case modeStandalone:
		runStandalone()
	default:
		printUsage()
		os.Exit(1)
	}
}

func runDaemon(args []string) {
	fmt.Println("TODO: runDaemon")
}

func runClient() {
	fmt.Println("TODO: runClient")
}

func runStandalone() {
	fmt.Println("TODO: runStandalone")
}
