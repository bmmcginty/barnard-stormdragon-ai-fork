//go:build windows

package main

import (
	"fmt"
	"strings"
)

// Windows named pipes require the Win32 API rather than POSIX filesystem
// FIFOs. Disable the legacy FIFO option until it is replaced with a named-pipe
// listener; ordinary UI and command-line operation remain available.
func setup_fifo(fn string) (chan string, error) {
	commands := make(chan string)
	if strings.TrimSpace(fn) == "" {
		return commands, nil
	}
	return commands, fmt.Errorf("FIFO control is not supported on Windows")
}
