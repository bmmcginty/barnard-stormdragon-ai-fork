package gumble

import (
	"bytes"
	"encoding/hex"
	"net"
	"time"

	"git.stormux.org/storm/barnard/log"
)

const (
	// maxUDPPacketSize is the maximum UDP packet size we'll process.
	maxUDPPacketSize = 1024
)

// startUDP initializes a UDP connection to the server and begins reading
// audio packets. It should be called after the server address is known.
func (c *Client) startUDP() error {
	addr := c.Conn.RemoteAddr()
	log.Debug("attempting UDP connection to %s", addr.String())

	udpAddr, err := net.ResolveUDPAddr("udp", addr.String())
	if err != nil {
		log.Warn("failed to resolve UDP address %s: %v", addr.String(), err)
		return err
	}

	conn, err := net.DialUDP("udp", nil, udpAddr)
	if err != nil {
		log.Warn("UDP dial failed (audio will use TCP tunnel): %v", err)
		return nil
	}

	c.udpMu.Lock()
	c.udpConn = conn
	c.udpMu.Unlock()
	log.Info("UDP socket connected to %s", conn.RemoteAddr())
	// The UDP reader and pinger will be started once CryptSetup is received.
	return nil
}

// udpReadRoutine reads encrypted UDP audio packets from the server.
// Uses Mumble 1.5 native UDP format.
func (c *Client) udpReadRoutine() {
	log.Info("UDP reader started (1.5 native format)")
	buf := make([]byte, maxUDPPacketSize)
	var packetCount uint64
	c.udpMu.RLock()
	udpConn := c.udpConn
	c.udpMu.RUnlock()
	if udpConn == nil {
		return
	}
	for {
		n, addr, err := udpConn.ReadFromUDP(buf)
		if err != nil {
			log.Warn("UDP read error (stopping reader): %v", err)
			return
		}
		packetCount++
		// A synchronous log write for every UDP datagram can itself make the
		// reader fall behind and lose voice packets. Keep enough samples to
		// diagnose framing while avoiding work on the audio hot path.
		if log.Enabled(log.LevelDebug) && (packetCount <= 3 || packetCount%1000 == 0) {
			log.Debug("UDP recv #%d: %d bytes from %s hex=%s",
				packetCount, n, addr, hex.EncodeToString(buf[:n]))
		}
		packet := make([]byte, n)
		copy(packet, buf[:n])
		c.HandleUDPPacket15(packet, packetCount)
	}
}

// udpPingRoutine sends periodic ping packets over UDP to keep the
// connection alive and maintain NAT bindings. Uses Mumble 1.5 native
// UDP ping format: type byte 0x01, protobuf field 1 = timestamp.
func (c *Client) udpPingRoutine() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.end:
			return
		case <-ticker.C:
			c.sendUDPPing()
		}
	}
}

// sendUDPPing sends a Mumble 1.5 native UDP ping.
// Uses standard protobuf varint encoding (not Mumble's custom varint).
func (c *Client) sendUDPPing() {
	c.udpWriteMu.Lock()
	defer c.udpWriteMu.Unlock()
	c.udpMu.RLock()
	cs, udpConn := c.udpCryptoOut, c.udpConn
	c.udpMu.RUnlock()
	if cs == nil || udpConn == nil {
		return
	}
	// Type byte 0x01 = UDPPing, field 1 = timestamp (protobuf varint, milliseconds).
	var tmp [10]byte // max protobuf varint size
	var buf bytes.Buffer
	buf.WriteByte(0x01) // type = UDPPing

	n := pbEncodeVarint(tmp[:], uint64((1<<3)|0))
	buf.Write(tmp[:n]) // field 1 tag

	n = pbEncodeVarint(tmp[:], uint64(time.Now().UnixMilli()))
	buf.Write(tmp[:n]) // timestamp value

	encrypted, err := cs.encrypt15(buf.Bytes())
	if err != nil {
		return
	}
	udpConn.Write(encrypted)
}
