//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package fileplayback

import (
	"os/exec"
	"testing"
)

func TestConfigureProcessGroupCreatesSeparateGroup(t *testing.T) {
	cmd := exec.Command("true")
	configureProcessGroup(cmd)
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setpgid {
		t.Fatal("ffmpeg process was not configured to lead its own process group")
	}
}
