package gumble

import (
	"crypto/tls"
	"errors"
	"fmt"
	"math"
	"net"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"git.stormux.org/storm/barnard/gumble/gumble/MumbleProto"
	"git.stormux.org/storm/barnard/log"
	"google.golang.org/protobuf/proto"
)

// State is the current state of the client's connection to the server.
type State int

const (
	// StateDisconnected means the client is no longer connected to the server.
	StateDisconnected State = iota

	// StateConnected means the client is connected to the server and is
	// syncing initial information. This is an internal state that will
	// never be returned by Client.State().
	StateConnected

	// StateSynced means the client is connected to a server and has been sent
	// the server state.
	StateSynced
)

// ClientVersion is the protocol version that Client implements.
const ClientVersion = 1<<16 | 5<<8 | 0

// Client is the type used to create a connection to a server.
type Client struct {
	// The User associated with the client.
	Self *User
	// The client's configuration.
	Config *Config
	// The underlying Conn to the server.
	Conn *Conn

	// The users currently connected to the server.
	Users Users
	// The connected server's channels.
	Channels    Channels
	permissions map[uint32]*Permission
	tmpACL      *ACL

	// Ping stats
	tcpPacketsReceived uint32
	tcpPingTimes       [12]float32
	tcpPingAvg         uint32
	tcpPingVar         uint32

	// A collection containing the server's context actions.
	ContextActions ContextActions

	// The audio encoder used when sending audio to the server.
	AudioEncoder       AudioEncoder
	AudioEncoderStereo AudioEncoder
	audioCodec         AudioCodec
	useStereoEncoder   bool
	// To whom transmitted audio will be sent. The VoiceTarget must have already
	// been sent to the server for targeting to work correctly. Setting to nil
	// will disable voice targeting (i.e. switch back to regular speaking).
	VoiceTarget *VoiceTarget

	// UDP transport for audio (lower latency than TCP-tunneled audio).
	udpMu             sync.RWMutex
	udpWriteMu        sync.Mutex
	udpConn           *net.UDPConn
	udpStarted        bool
	udpActive         bool
	udpCryptoOut      *cryptState15
	udpCryptoIn       *cryptState15
	udpFrameNumber    uint64
	udpProtobuf       bool
	udpFallbackLogged atomic.Bool
	udpFirstRecv      atomic.Bool
	cryptOut          cryptState // client→server encryption
	cryptIn           cryptState // server→client encryption

	state uint32

	// volatile is held by the client when the internal data structures are being
	// modified.
	volatile rpwMutex

	connect         chan *RejectError
	end             chan struct{}
	disconnectEvent DisconnectEvent
}

// Dial is an alias of DialWithDialer(new(net.Dialer), config, nil).
func Dial(config *Config) (*Client, error) {
	return DialWithDialer(new(net.Dialer), config, nil)
}

// tlsServerName returns the hostname portion of a Mumble server address for
// TLS certificate verification and SNI.
func tlsServerName(address string) (string, error) {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return "", fmt.Errorf("gumble: derive TLS server name from %q: %w", address, err)
	}
	if host == "" {
		return "", fmt.Errorf("gumble: derive TLS server name from %q: empty host", address)
	}
	return host, nil
}

// DialWithDialer connects to the Mumble server at the address given in config.
//
// The function returns after the connection has been established, the initial
// server information has been synced, and the OnConnect handlers have been
// called.
//
// nil and an error is returned if server synchronization does not complete by
// min(time.Now() + dialer.Timeout, dialer.Deadline), or if the server rejects
// the client.
func DialWithDialer(dialer *net.Dialer, config *Config, tlsConfig *tls.Config) (*Client, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	start := time.Now()

	rawConn, err := dialer.Dial("tcp", config.Address)
	if err != nil {
		return nil, err
	}

	// tls.Client cannot infer a server name from an already-open connection.
	// Clone the caller's configuration before deriving it so reconnects and
	// concurrent clients do not mutate a shared configuration.
	if tlsConfig == nil {
		tlsConfig = &tls.Config{}
	} else {
		tlsConfig = tlsConfig.Clone()
	}
	if tlsConfig.ServerName == "" {
		serverName, err := tlsServerName(config.Address)
		if err != nil {
			rawConn.Close()
			return nil, err
		}
		tlsConfig.ServerName = serverName
	}
	conn := tls.Client(rawConn, tlsConfig)
	// net.Dialer.Timeout covers only the TCP dial. Apply the same bounded
	// deadline to TLS negotiation so a peer that accepts but never responds
	// cannot block startup indefinitely.
	if dialer.Timeout > 0 {
		if err := conn.SetDeadline(start.Add(dialer.Timeout)); err != nil {
			rawConn.Close()
			return nil, err
		}
	}
	if err := conn.Handshake(); err != nil {
		rawConn.Close()
		return nil, err
	}
	if err := conn.SetDeadline(time.Time{}); err != nil {
		rawConn.Close()
		return nil, err
	}

	client := &Client{
		Conn:           NewConn(conn),
		Config:         config,
		Users:          make(Users),
		Channels:       make(Channels),
		ContextActions: make(ContextActions),

		permissions: make(map[uint32]*Permission),

		state: uint32(StateConnected),

		connect: make(chan *RejectError),
		end:     make(chan struct{}),
	}

	go client.readRoutine()

	// Initial packets
	versionPacket := MumbleProto.Version{
		VersionV1: proto.Uint32(ClientVersion),
		Release:   proto.String("gumble"),
		Os:        proto.String(runtime.GOOS),
		OsVersion: proto.String(runtime.GOARCH),
	}
	authenticationPacket := MumbleProto.Authenticate{
		Username: &client.Config.Username,
		Password: &client.Config.Password,
		Opus:     proto.Bool(getAudioCodec(audioCodecIDOpus) != nil),
		Tokens:   client.Config.Tokens,
	}
	client.Conn.WriteProto(&versionPacket)
	client.Conn.WriteProto(&authenticationPacket)

	// Start UDP transport immediately so it's ready when CryptSetup
	// arrives during the sync handshake.
	if !client.Config.DisableUDP {
		if err := client.startUDP(); err != nil {
			log.Warn("UDP setup failed, audio will use TCP tunnel: %v", err)
		} else if client.udpConn != nil {
			log.Info("UDP socket opened to %s, waiting for CryptSetup", client.udpConn.RemoteAddr())
		}
	} else {
		log.Info("UDP disabled by config, audio will use TCP tunnel")
	}

	go client.pingRoutine()

	var timeout <-chan time.Time
	{
		var deadline time.Time
		if !dialer.Deadline.IsZero() {
			deadline = dialer.Deadline
		}
		if dialer.Timeout > 0 {
			diff := start.Add(dialer.Timeout)
			if deadline.IsZero() || diff.Before(deadline) {
				deadline = diff
			}
		}
		if !deadline.IsZero() {
			timer := time.NewTimer(deadline.Sub(start))
			defer timer.Stop()
			timeout = timer.C
		}
	}

	select {
	case <-timeout:
		client.Conn.Close()
		return nil, errors.New("gumble: synchronization timeout")
	case err := <-client.connect:
		if err != nil {
			client.Conn.Close()
			return nil, err
		}

		return client, nil
	}
}

// State returns the current state of the client.
func (c *Client) State() State {
	return State(atomic.LoadUint32(&c.state))
}

// AudioOutgoing creates a new channel that outgoing audio data can be written
// to. The channel must be closed after the audio stream is completed. Only
// a single channel should be open at any given time (i.e. close the channel
// before opening another).
func (c *Client) AudioOutgoing() chan<- AudioBuffer {
	ch := make(chan AudioBuffer)
	go func() {
		var seq int64
		previous := <-ch
		for p := range ch {
			previous.writeAudio(c, seq, false)
			previous = p
			seq = (seq + 1) % math.MaxInt32
		}
		if previous != nil {
			previous.writeAudio(c, seq, true)
		}
	}()
	return ch
}

// pingRoutine sends ping packets to the server at regular intervals.
func (c *Client) pingRoutine() {
	ticker := time.NewTicker(time.Second * 5)
	defer ticker.Stop()

	var timestamp uint64
	var tcpPingAvg float32
	var tcpPingVar float32
	packet := MumbleProto.Ping{
		Timestamp:  &timestamp,
		TcpPackets: &c.tcpPacketsReceived,
		TcpPingAvg: &tcpPingAvg,
		TcpPingVar: &tcpPingVar,
	}

	t := time.Now()
	for {
		timestamp = uint64(t.UnixNano())
		tcpPingAvg = math.Float32frombits(atomic.LoadUint32(&c.tcpPingAvg))
		tcpPingVar = math.Float32frombits(atomic.LoadUint32(&c.tcpPingVar))
		c.Conn.WriteProto(&packet)

		select {
		case <-c.end:
			return
		case t = <-ticker.C:
			// continue to top of loop
		}
	}
}

// readRoutine reads protocol buffer messages from the server.
func (c *Client) readRoutine() {
	c.disconnectEvent = DisconnectEvent{
		Client: c,
		Type:   DisconnectError,
	}

	for {
		pType, data, err := c.Conn.ReadPacket()
		if err != nil {
			break
		}
		// When UDP audio is active, ignore TCP-tunneled audio
		// (packet type 1) to avoid double-processing packets.
		c.udpMu.RLock()
		udpActive := c.udpActive
		c.udpMu.RUnlock()
		if pType == 1 && udpActive {
			continue
		}
		if int(pType) < len(handlers) {
			handlers[pType](c, data)
		}
	}

	wasSynced := c.State() == StateSynced
	atomic.StoreUint32(&c.state, uint32(StateDisconnected))
	close(c.end)

	// Clean up UDP connection.
	c.udpMu.Lock()
	udpConn := c.udpConn
	c.udpConn = nil
	c.udpActive = false
	c.udpMu.Unlock()
	if udpConn != nil {
		log.Debug("closing UDP connection")
		udpConn.Close()
	}

	if wasSynced {
		c.Config.Listeners.onDisconnect(&c.disconnectEvent)
	}
}

// RequestUserList requests that the server's registered user list be sent to
// the client.
func (c *Client) RequestUserList() {
	packet := MumbleProto.UserList{}
	c.Conn.WriteProto(&packet)
}

// RequestBanList requests that the server's ban list be sent to the client.
func (c *Client) RequestBanList() {
	packet := MumbleProto.BanList{
		Query: proto.Bool(true),
	}
	c.Conn.WriteProto(&packet)
}

// Disconnect disconnects the client from the server.
func (c *Client) Disconnect() error {
	if c.State() == StateDisconnected {
		return errors.New("gumble: client is already disconnected")
	}
	c.disconnectEvent.Type = DisconnectUser
	c.Conn.Close()
	return nil
}

// Do executes f in a thread-safe manner. It ensures that Client and its
// associated data will not be changed during the lifetime of the function
// call.
func (c *Client) Do(f func()) {
	c.volatile.RLock()
	defer c.volatile.RUnlock()

	f()
}

// Send will send a Message to the server.
func (c *Client) Send(message Message) {
	message.writeMessage(c)
}

// EnableStereoEncoder switches to stereo encoding for file playback.
func (c *Client) EnableStereoEncoder() {
	c.volatile.Lock()
	defer c.volatile.Unlock()
	c.useStereoEncoder = true
}

// WriteAudio writes an audio packet, preferring UDP when encryption is
// set up. Falls back to TCP-tunneled audio when UDP is unavailable.

func (c *Client) WriteAudio(format, target byte, sequence int64, final bool, data []byte, X, Y, Z *float32) error {
	// Try Mumble 1.5 native UDP first (unless disabled)
	if !c.Config.DisableUDP {
		if sent, err := c.WriteAudioUDP15(format, uint32(target), sequence, data, final, X, Y, Z); sent {
			if err != nil {
				log.Error("UDP15 send error: %v", err)
			}
			return err
		}
	}
	// Fall back to the TCP tunnel.
	c.udpMu.RLock()
	udpConn := c.udpConn
	udpCryptoOut := c.udpCryptoOut
	udpProtobuf := c.udpProtobuf
	c.udpMu.RUnlock()
	if !c.udpFallbackLogged.Swap(true) {
		if c.Config.DisableUDP {
			log.Info("UDP disabled, audio using TCP tunnel")
		} else if udpConn == nil {
			log.Info("no UDP socket, audio using TCP tunnel")
		} else if udpCryptoOut == nil {
			log.Info("UDP crypto not ready, audio using TCP tunnel")
		}
	}
	if udpProtobuf {
		// Mumble 1.5 uses the native UDP protobuf envelope even when audio is
		// carried inside the TCP UDPTunnel packet.
		payload := append([]byte{0x00}, encodeUDPAudio(uint32(target), uint64(sequence), data, final, X, Y, Z)...)
		return c.Conn.WritePacket(1, payload)
	}
	return c.Conn.WriteAudio(format, target, sequence, final, data, X, Y, Z)
}

// UDPActive reports whether an authenticated UDP packet has confirmed the
// return path and outgoing audio may use native UDP.
func (c *Client) UDPActive() bool {
	c.udpMu.RLock()
	defer c.udpMu.RUnlock()
	return c.udpActive
}

// DisableStereoEncoder switches back to mono encoding for voice.
func (c *Client) DisableStereoEncoder() {
	c.volatile.Lock()
	defer c.volatile.Unlock()
	c.useStereoEncoder = false
}

// IsStereoEncoderEnabled returns true if stereo encoding is currently active.
func (c *Client) IsStereoEncoderEnabled() bool {
	c.volatile.RLock()
	defer c.volatile.RUnlock()
	return c.useStereoEncoder
}
