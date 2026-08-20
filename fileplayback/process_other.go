//go:build !(aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris)

package fileplayback

import "os/exec"

func configureProcessGroup(cmd *exec.Cmd) {}

func terminateProcessGroup(cmd *exec.Cmd) {
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}
