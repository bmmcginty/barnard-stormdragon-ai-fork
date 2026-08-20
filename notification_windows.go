//go:build windows

package main

import "os/exec"

func runNotification(command string) {
	_ = exec.Command("cmd.exe", "/C", command).Run()
}
