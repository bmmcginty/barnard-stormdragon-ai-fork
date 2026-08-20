package config

import (
	"fmt"
	"git.stormux.org/storm/barnard/gumble/gumble"
	"git.stormux.org/storm/barnard/uiterm"
	"github.com/pelletier/go-toml/v2"
	"io/ioutil"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

type Config struct {
	mu     sync.Mutex
	config *exportableConfig
	fn     string
}

type exportableConfig struct {
	Hotkeys                 *Hotkeys
	AudioDriver             *string
	MicVolume               *float32
	InputDevice             *string
	OutputDevice            *string
	Servers                 []*server
	DefaultServer           *string
	Username                *string
	NotifyCommand           *string
	NoiseSuppressionEnabled *bool
	Certificate             *string
	RecordingFormat         *string
	RecordingDirectory      *string
}

type server struct {
	Host  string
	Port  int
	Users []*eUser
}

type eUser struct {
	Username     string
	Boost        uint16
	Volume       float32
	LocallyMuted bool // Changed from Muted to LocallyMuted to match User struct
}

// SaveConfig atomically replaces the persisted configuration. Errors are
// returned so an unavailable directory cannot crash the client.
func (c *Config) SaveConfig() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.saveConfigLocked()
}

func (c *Config) saveConfigLocked() error {
	if err := os.MkdirAll(filepath.Dir(c.fn), 0700); err != nil {
		return err
	}
	data, err := toml.Marshal(c.config)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(c.fn), filepath.Base(c.fn)+".tmp-")
	if err != nil {
		return err
	}
	tmp := file.Name()
	defer os.Remove(tmp)
	if err := file.Chmod(0600); err != nil {
		file.Close()
		return err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, c.fn)
}

func key(k uiterm.Key) *uiterm.Key {
	return &k
}

func (c *Config) LoadConfig() {
	var jc exportableConfig
	jc = exportableConfig{}
	jc.Hotkeys = &Hotkeys{
		Talk:                   key(uiterm.KeyF1),
		VolumeDown:             key(uiterm.KeyF5),
		VolumeUp:               key(uiterm.KeyF6),
		VolumeReset:            key(uiterm.KeyF8),
		MuteToggle:             key(uiterm.KeyF7), // Added mute toggle hotkey
		RecordToggle:           key(uiterm.KeyCtrlR),
		Exit:                   key(uiterm.KeyF10),
		ToggleTimestamps:       key(uiterm.KeyF3),
		SwitchViews:            key(uiterm.KeyTab),
		ClearOutput:            key(uiterm.KeyCtrlL),
		ScrollUp:               key(uiterm.KeyPgup),
		ScrollDown:             key(uiterm.KeyPgdn),
		ScrollToTop:            key(uiterm.KeyHome),
		ScrollToBottom:         key(uiterm.KeyEnd),
		AdminMenu:              key(uiterm.KeyF11),
		NoiseSuppressionToggle: key(uiterm.KeyF9),
	}
	if fileExists(c.fn) {
		var data []byte
		data = readFile(c.fn)
		if data != nil {
			err := toml.Unmarshal(data, &jc)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error parsing \"%s\".\n%s\n", c.fn, err.Error())
				os.Exit(1)
			}
		}
	}
	c.config = &jc
	c.ensureHotkeys()
	if c.config.MicVolume == nil {
		micvol := float32(1.0)
		jc.MicVolume = &micvol
	}
	if c.config.AudioDriver == nil {
		driver := string("")
		jc.AudioDriver = &driver
	}
	if c.config.InputDevice == nil {
		idev := string("")
		jc.InputDevice = &idev
	}
	if c.config.OutputDevice == nil {
		odev := string("")
		jc.OutputDevice = &odev
	}
	if c.config.DefaultServer == nil {
		defaultServer := string("localhost:64738")
		jc.DefaultServer = &defaultServer
	}
	if c.config.Username == nil {
		username := string("")
		jc.Username = &username
	}
	if c.config.NotifyCommand == nil {
		ncmd := string("/usr/share/barnard/barnard-sound.sh \"%event\" \"%who\" \"%what\"")
		jc.NotifyCommand = &ncmd
	}
	if c.config.NoiseSuppressionEnabled == nil {
		enabled := false
		jc.NoiseSuppressionEnabled = &enabled
	}
	if c.config.Certificate == nil {
		cert := string("")
		jc.Certificate = &cert
	}
	if c.config.RecordingFormat == nil {
		format := string("flac")
		jc.RecordingFormat = &format
	}
	if c.config.RecordingDirectory == nil {
		dir := string("~/Audio")
		jc.RecordingDirectory = &dir
	}
}

func (c *Config) ensureHotkeys() {
	if c.config.Hotkeys == nil {
		c.config.Hotkeys = &Hotkeys{}
	}
	defaults := Hotkeys{
		Talk:                   key(uiterm.KeyF1),
		VolumeDown:             key(uiterm.KeyF5),
		VolumeUp:               key(uiterm.KeyF6),
		VolumeReset:            key(uiterm.KeyF8),
		MuteToggle:             key(uiterm.KeyF7),
		RecordToggle:           key(uiterm.KeyCtrlR),
		Exit:                   key(uiterm.KeyF10),
		ToggleTimestamps:       key(uiterm.KeyF3),
		SwitchViews:            key(uiterm.KeyTab),
		ClearOutput:            key(uiterm.KeyCtrlL),
		ScrollUp:               key(uiterm.KeyPgup),
		ScrollDown:             key(uiterm.KeyPgdn),
		ScrollToTop:            key(uiterm.KeyHome),
		ScrollToBottom:         key(uiterm.KeyEnd),
		AdminMenu:              key(uiterm.KeyF11),
		NoiseSuppressionToggle: key(uiterm.KeyF9),
	}
	hotkeys := c.config.Hotkeys
	if hotkeys.Talk == nil {
		hotkeys.Talk = defaults.Talk
	}
	if hotkeys.VolumeDown == nil {
		hotkeys.VolumeDown = defaults.VolumeDown
	}
	if hotkeys.VolumeUp == nil {
		hotkeys.VolumeUp = defaults.VolumeUp
	}
	if hotkeys.VolumeReset == nil {
		hotkeys.VolumeReset = defaults.VolumeReset
	}
	if hotkeys.MuteToggle == nil {
		hotkeys.MuteToggle = defaults.MuteToggle
	}
	if hotkeys.RecordToggle == nil {
		hotkeys.RecordToggle = defaults.RecordToggle
	}
	if hotkeys.Exit == nil {
		hotkeys.Exit = defaults.Exit
	}
	if hotkeys.ToggleTimestamps == nil {
		hotkeys.ToggleTimestamps = defaults.ToggleTimestamps
	}
	if hotkeys.SwitchViews == nil {
		hotkeys.SwitchViews = defaults.SwitchViews
	}
	if hotkeys.ClearOutput == nil {
		hotkeys.ClearOutput = defaults.ClearOutput
	}
	if hotkeys.ScrollUp == nil {
		hotkeys.ScrollUp = defaults.ScrollUp
	}
	if hotkeys.ScrollDown == nil {
		hotkeys.ScrollDown = defaults.ScrollDown
	}
	if hotkeys.ScrollToTop == nil {
		hotkeys.ScrollToTop = defaults.ScrollToTop
	}
	if hotkeys.ScrollToBottom == nil {
		hotkeys.ScrollToBottom = defaults.ScrollToBottom
	}
	if hotkeys.AdminMenu == nil {
		hotkeys.AdminMenu = defaults.AdminMenu
	}
	if hotkeys.NoiseSuppressionToggle == nil {
		hotkeys.NoiseSuppressionToggle = defaults.NoiseSuppressionToggle
	}
}

func (c *Config) findServer(address string) *server {
	if c.config.Servers == nil {
		c.config.Servers = make([]*server, 0)
	}
	host, port := makeHostPort(address)
	var t *server
	for _, s := range c.config.Servers {
		if s.Port == port && s.Host == host {
			t = s
			break
		}
	}
	if t == nil {
		t = &server{
			Host: host,
			Port: port,
		}
		c.config.Servers = append(c.config.Servers, t)
	}
	return t
}

func (c *Config) findUser(address string, username string) *eUser {
	var s *server
	s = c.findServer(address)
	if s.Users == nil {
		s.Users = make([]*eUser, 0)
	}
	var t *eUser
	for _, u := range s.Users {
		if u.Username == username {
			t = u
			break
		}
	}
	if t == nil {
		t = &eUser{
			Username:     username,
			Boost:        uint16(1),
			Volume:       1.0,
			LocallyMuted: false, // Initialize local mute state
		}
		s.Users = append(s.Users, t)
	}
	return t
}

func (c *Config) ToggleMute(u *gumble.User) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	j := c.findUser(u.GetClient().Config.Address, u.Name)
	j.LocallyMuted = !j.LocallyMuted
	u.SetLocallyMuted(j.LocallyMuted)
	return c.saveConfigLocked()
}

func (c *Config) SetMicVolume(v float32) {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := float32(v)
	c.config.MicVolume = &t
}

func (c *Config) GetHotkeys() *Hotkeys {
	return c.config.Hotkeys
}

func (c *Config) GetNotifyCommand() *string {
	return c.config.NotifyCommand
}

func (c *Config) GetAudioDriver() string {
	if c.config.AudioDriver == nil {
		return ""
	}
	return *c.config.AudioDriver
}

func (c *Config) GetInputDevice() *string {
	return c.config.InputDevice
}

func (c *Config) GetOutputDevice() *string {
	return c.config.OutputDevice
}

func (c *Config) GetDefaultServer() *string {
	return c.config.DefaultServer
}

func (c *Config) GetUsername() *string {
	return c.config.Username
}

func (c *Config) GetCertificate() *string {
	return c.config.Certificate
}

func (c *Config) GetNoiseSuppressionEnabled() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.config.NoiseSuppressionEnabled == nil {
		return false
	}
	return *c.config.NoiseSuppressionEnabled
}

func (c *Config) SetNoiseSuppressionEnabled(enabled bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.config.NoiseSuppressionEnabled = &enabled
	return c.saveConfigLocked()
}

func (c *Config) GetRecordingFormat() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.config.RecordingFormat == nil {
		return "flac"
	}
	return strings.ToLower(strings.TrimSpace(*c.config.RecordingFormat))
}

func (c *Config) GetRecordingDirectory() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.config.RecordingDirectory == nil {
		return resolvePath("~/Audio")
	}
	return resolvePath(*c.config.RecordingDirectory)
}

func (c *Config) UpdateUser(u *gumble.User) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var j *eUser
	var uc *gumble.Client
	uc = u.GetClient()
	if uc != nil {
		j = c.findUser(uc.Config.Address, u.Name)
		u.SetBoost(j.Boost)
		u.SetVolume(j.Volume)
		u.SetLocallyMuted(j.LocallyMuted) // Update LocallyMuted state from config
		if u.Boost() < 1 {
			u.SetBoost(1)
		}
	}
}

func (c *Config) UpdateConfig(u *gumble.User) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var j *eUser
	j = c.findUser(u.GetClient().Config.Address, u.Name)
	j.Boost = u.Boost()
	j.Volume = u.Volume()
	j.LocallyMuted = u.LocallyMuted() // Save LocallyMuted state to config
}

// RequireConfigFile verifies that an explicitly requested configuration file
// exists and is a regular file. The default configuration remains optional.
func RequireConfigFile(fn string) error {
	path := resolvePath(fn)
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("config file %q: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("config file %q is not a regular file", path)
	}
	return nil
}

func NewConfig(fn *string) *Config {
	var c *Config
	c = &Config{}
	c.fn = resolvePath(*fn)
	c.LoadConfig()
	return c
}

func readFile(path string) []byte {
	if !fileExists(path) {
		return nil
	}
	dat, err := ioutil.ReadFile(path)
	if err != nil {
		return nil
	}
	return dat
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

func resolvePath(path string) string {
	if strings.HasPrefix(path, "~/") || strings.Contains(path, "$HOME") {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			panic(err)
		}
		if strings.Contains(path, "$HOME") {
			path = strings.Replace(path, "$HOME", homeDir, 1)
		} else {
			path = strings.Replace(path, "~", homeDir, 1)
		}
	}
	return path
}

func makeHostPort(addr string) (string, int) {
	// SplitHostPort correctly handles bracketed IPv6. Invalid or portless
	// addresses stay usable as a host with Mumble's default port instead of
	// crashing configuration operations.
	host, portText, err := net.SplitHostPort(addr)
	if err != nil {
		return strings.Trim(addr, "[]"), 64738
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return host, 64738
	}
	return host, port
}

func Log(s string) {
	log(s)
}

func log(s string) {
	s += "\n"
	f, err := os.OpenFile("log.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		panic(err)
	}
	if _, err := f.Write([]byte(s)); err != nil {
		panic(err)
	}
	if err := f.Close(); err != nil {
		panic(err)
	}
}
