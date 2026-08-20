package main

import _ "net/http/pprof"
import (
	"al.essio.dev/pkg/shellescape"
	"bufio"
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"syscall"

	barnlog "git.stormux.org/storm/barnard/log"

	"git.stormux.org/storm/barnard/config"
	"git.stormux.org/storm/barnard/gumble/go-openal/openal"
	"git.stormux.org/storm/barnard/gumble/gumble"
	_ "git.stormux.org/storm/barnard/gumble/opus"
	"git.stormux.org/storm/barnard/noise"
	"git.stormux.org/storm/barnard/uiterm"
)

func show_devs(name string, args []string) {
	if args == nil {
		fmt.Printf("no items for %s\n", name)
	}
	fmt.Printf("%s\n", name)
	for i := 0; i < len(args); i++ {
		fmt.Printf("%s\n", args[i])
	}
}

func do_list_devices() {
	odevs := openal.GetStrings(openal.AllDevicesSpecifier)
	if odevs != nil && len(odevs) > 0 {
		show_devs("All outputs:", odevs)
	} else {
		odevs = openal.GetStrings(openal.DeviceSpecifier)
		show_devs("All outputs:", odevs)
	}
	idevs := openal.GetStrings(openal.CaptureDeviceSpecifier)
	show_devs("Inputs:", idevs)
}

func setup_notify_runner(notify_command string) chan []string {
	t := make(chan []string)
	var do_nothing = false
	var err error
	if err != nil {
	}
	if notify_command == "" {
		do_nothing = true
	}
	go func(events chan []string, cmd_template string, dummy bool) {
		for {
			event := <-events
			if !dummy {
				t := string(cmd_template)
				t = strings.ReplaceAll(t, "%event", shellescape.Quote(event[0]))
				t = strings.ReplaceAll(t, "%who", shellescape.Quote(event[1]))
				t = strings.ReplaceAll(t, "%what", shellescape.Quote(event[2]))
				cmd := "/bin/sh"
				args := []string{"-c", t}
				x := exec.Command(cmd, args...)
				x.Run()
			} //if we actually have a command to run
		} //for
	}(t, notify_command, do_nothing)
	return t
}

func setup_fifo(fn string) (chan string, error) {
	t := make(chan string)
	if fn == "" {
		return t, nil
	}
	os.Remove(fn)
	err := syscall.Mkfifo(fn, 0600)
	if err != nil {
		return t, err
	}
	file, err := os.OpenFile(fn, os.O_RDWR, os.ModeNamedPipe)
	if err != nil {
		return t, err
	}
	go func(fh io.Reader, out chan string) {
		reader := bufio.NewReader(fh)
		for {
			line, err := reader.ReadBytes('\n')
			if err == nil {
				out <- strings.TrimSpace(string(line))
			}
		}
	}(file, t)
	return t, nil
}

func main() {
	// Command line flags
	server := flag.String("server", "localhost:64738", "the server to connect to")
	username := flag.String("username", "", "the username of the client")
	password := flag.String("password", "", "the password of the server")
	insecure := flag.Bool("insecure", false, "skip server certificate verification")
	certificate := flag.String("certificate", "", "PEM encoded certificate and private key")
	cfgfn := flag.String("config", "~/.barnard.toml", "Path to TOML formatted configuration file")
	audioDriver := flag.String("audio-driver", "", "preferred OpenAL backend (pipewire, pulse, alsa, jack)")
	list_devices := flag.Bool("list_devices", false, "do not connect; instead, list available audio devices and exit")
	fifo := flag.String("fifo", "", "path of a FIFO from which to read commands")
	serverSet := false
	usernameSet := false
	buffers := flag.Int("buffers", 16, "number of audio buffers to use")
	profile := flag.Bool("profile", false, "add http server to serve profiles")
	noiseSuppressionEnabled := flag.Bool("noise-suppression", false, "enable noise suppression for microphone input")
	tcpOnly := flag.Bool("tcp", false, "disable UDP, force audio through TCP tunnel")
	logLevel := flag.String("log", "warn", "log level: debug, info, warn, error")
	logFile := flag.String("logfile", "", "write logs to this file (logging is disabled when omitted)")

	flag.Parse()

	// Set up logging
	var level barnlog.Level
	switch strings.ToLower(*logLevel) {
	case "debug":
		level = barnlog.LevelDebug
	case "info":
		level = barnlog.LevelInfo
	case "warn":
		level = barnlog.LevelWarn
	case "error":
		level = barnlog.LevelError
	default:
		level = barnlog.LevelWarn
	}
	// Logging is opt-in. Select /dev/stderr explicitly when terminal logging is
	// desired; otherwise library diagnostics must not corrupt terminal output.
	barnlog.SetLogger(nil)
	if *logFile != "" {
		f, err := os.OpenFile(*logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			fmt.Fprintf(os.Stderr, "cannot open log file %s: %v\n", *logFile, err)
		} else {
			barnlog.SetLogger(barnlog.NewWriterLogger(f, level))
		}
	}

	if *profile == true {
		go func() {
			log.Println(http.ListenAndServe("localhost:6060", nil))
		}()
	}

	userConfig := config.NewConfig(cfgfn)

	certificateSet := false
	flag.CommandLine.Visit(func(theFlag *flag.Flag) {
		switch theFlag.Name {
		case "server":
			serverSet = true
		case "username":
			usernameSet = true
		case "certificate":
			certificateSet = true
		}
	})

	if !serverSet {
		server = userConfig.GetDefaultServer()
	}
	if !usernameSet {
		username = userConfig.GetUsername()
	}
	if !certificateSet {
		certificate = userConfig.GetCertificate()
	}

	driver := strings.TrimSpace(*audioDriver)
	if driver == "" {
		// Environment variable takes precedence over config
		if envDriver := os.Getenv("ALSOFT_DRIVERS"); envDriver != "" {
			driver = envDriver
		} else {
			driver = strings.TrimSpace(userConfig.GetAudioDriver())
		}
	}
	if driver != "" {
		os.Setenv("ALSOFT_DRIVERS", driver)
	}

	if os.Getenv("ALSOFT_LOGLEVEL") == "" {
		os.Setenv("ALSOFT_LOGLEVEL", "0")
	}

	if *list_devices {
		do_list_devices()
		os.Exit(0)
	}

	if !strings.Contains(*server, ":") {
		*server = (*server + ":64738")
	}

	// Initialize
	b := Barnard{
		Config:          gumble.NewConfig(),
		UserConfig:      userConfig,
		Address:         *server,
		MutedChannels:   make(map[uint32]bool),
		NoiseSuppressor: noise.NewSuppressor(),
	}
	b.Config.Buffers = *buffers
	b.Config.DisableUDP = *tcpOnly

	b.Hotkeys = b.UserConfig.GetHotkeys()
	b.UserConfig.SaveConfig()

	// Configure noise suppression
	enabled := b.UserConfig.GetNoiseSuppressionEnabled()
	if *noiseSuppressionEnabled {
		enabled = true
		b.UserConfig.SetNoiseSuppressionEnabled(true)
	}
	b.NoiseSuppressor.SetEnabled(enabled)

	b.Config.Username = *username
	b.Config.Password = *password

	if *insecure {
		b.TLSConfig.InsecureSkipVerify = true
	}
	if *certificate != "" {
		cert, err := tls.LoadX509KeyPair(*certificate, *certificate)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s\n", err)
			os.Exit(1)
		}
		b.TLSConfig.Certificates = append(b.TLSConfig.Certificates, cert)
	}

	reader, err := setup_fifo(*fifo)
	if err != nil {
		b.exitMessage = err.Error()
		b.exitStatus = 1
		handle_error(&b)
	}
	b.notifyChannel = setup_notify_runner(*b.UserConfig.GetNotifyCommand())
	b.Ui = uiterm.New(&b)
	b.Ui.Run(reader)
	handle_error(&b)
}

func handle_raw_error(e error) {
	fmt.Fprintf(os.Stderr, "%s\n", e.Error())
	os.Exit(1)
}

func handle_error(b *Barnard) {
	if b.exitMessage != "" {
		fmt.Fprintf(os.Stderr, "%s\n", b.exitMessage)
	}
	os.Exit(b.exitStatus)
}
