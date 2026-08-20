//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"
)

func setup_fifo(fn string) (chan string, error) {
	commands := make(chan string)
	if fn == "" {
		return commands, nil
	}
	if info, err := os.Lstat(fn); err == nil {
		if info.Mode()&os.ModeNamedPipe == 0 {
			return commands, fmt.Errorf("FIFO path %q already exists and is not a FIFO", fn)
		}
		if err := os.Remove(fn); err != nil {
			return commands, err
		}
	} else if !os.IsNotExist(err) {
		return commands, err
	}
	if err := syscall.Mkfifo(fn, 0600); err != nil {
		return commands, err
	}
	file, err := os.OpenFile(fn, os.O_RDWR, os.ModeNamedPipe)
	if err != nil {
		return commands, err
	}
	go readFIFO(file, commands)
	return commands, nil
}

// readFIFO forwards complete commands and terminates on EOF or any read
// failure. Retrying an unrecoverable FIFO error used to spin a CPU forever.
func readFIFO(fh io.ReadCloser, out chan<- string) {
	defer fh.Close()
	defer close(out)
	reader := bufio.NewReader(fh)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) != 0 {
			out <- strings.TrimSpace(string(line))
		}
		if err != nil {
			return
		}
	}
}
