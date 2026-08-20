//go:build windows

package config

// Windows installations do not ship the POSIX notification helper. Users can
// configure a cmd.exe-compatible command explicitly if they want notifications.
func defaultNotifyCommand() string {
	return ""
}
