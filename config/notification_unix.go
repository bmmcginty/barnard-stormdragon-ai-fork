//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package config

func defaultNotifyCommand() string {
	return "/usr/share/barnard/barnard-sound.sh \"%event\" \"%who\" \"%what\""
}
