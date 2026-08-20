//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package main

import "os/exec"

func runNotification(command string) {
	_ = exec.Command("/bin/sh", "-c", command).Run()
}
