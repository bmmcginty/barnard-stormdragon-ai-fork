package gumble

import (
	"bytes"
	"crypto/aes"
	"crypto/subtle"
	"encoding/binary"
	"errors"
	"math"
	"sync"

	"git.stormux.org/storm/barnard/gumble/gumble/varint"
	"git.stormux.org/storm/barnard/log"
)

// ---------------------------------------------------------------------------
// Protobuf varint helpers.
// The 1.5 UDP protocol uses standard Google protobuf varint encoding,
// NOT Mumble's custom varint (which is used in the legacy UDP format).
// The Mumble custom varint is in ../varint/; protobuf varint is below.
// ---------------------------------------------------------------------------

// pbEncodeVarint writes v as a protobuf varint into buf and returns the
// number of bytes written.  buf must have sufficient space (10 bytes for
// a full uint64).
func pbEncodeVarint(buf []byte, v uint64) int {
	i := 0
	for v >= 0x80 {
		buf[i] = byte(v) | 0x80
		v >>= 7
		i++
	}
	buf[i] = byte(v)
	return i + 1
}

// pbDecodeVarint reads a protobuf varint from buf and returns the value
// and the number of bytes consumed (0 on error).
func pbDecodeVarint(buf []byte) (uint64, int) {
	var v uint64
	for i, b := range buf {
		if i == 10 || (i == 9 && b > 1) {
			return 0, 0 // overflow
		}
		v |= uint64(b&0x7F) << (7 * i)
		if b < 0x80 {
			return v, i + 1
		}
	}
	return 0, 0 // truncated
}

// ---------------------------------------------------------------------------
// Mumble 1.5 native UDP — MumbleUDP.Audio protobuf helpers.
//
// Message MumbleUDP.Audio:
//   field 3: sender_session  (varint, wire 0)
//   field 4: frame_number    (varint, wire 0)  — 10 ms units
//   field 5: opus_data       (bytes,  wire 2)
//   field 16: is_terminator  (varint, wire 0)
// ---------------------------------------------------------------------------

// encodeUDPAudio builds a MumbleUDP.Audio protobuf message.
// If session == 0, sender_session is omitted (used for outbound).
// Uses standard protobuf varint encoding, not Mumble's custom varint.
func encodeUDPAudio(target uint32, frameNumber uint64, opusData []byte, terminator bool, X, Y, Z *float32) []byte {
	var buf bytes.Buffer
	var tmp [10]byte // max protobuf varint size

	// Field 1 selects the target header oneof.
	n := pbEncodeVarint(tmp[:], uint64((1<<3)|0))
	buf.Write(tmp[:n])
	n = pbEncodeVarint(tmp[:], uint64(target))
	buf.Write(tmp[:n])

	n = pbEncodeVarint(tmp[:], uint64((4<<3)|0))
	buf.Write(tmp[:n])
	n = pbEncodeVarint(tmp[:], uint64(frameNumber))
	buf.Write(tmp[:n])
	if len(opusData) > 0 {
		n := pbEncodeVarint(tmp[:], uint64((5<<3)|2))
		buf.Write(tmp[:n])
		n = pbEncodeVarint(tmp[:], uint64(len(opusData)))
		buf.Write(tmp[:n])
		buf.Write(opusData)
	}
	if X != nil && Y != nil && Z != nil {
		n := pbEncodeVarint(tmp[:], uint64((6<<3)|2))
		buf.Write(tmp[:n])
		n = pbEncodeVarint(tmp[:], 12)
		buf.Write(tmp[:n])
		for _, value := range []float32{*X, *Y, *Z} {
			var fixed [4]byte
			binary.LittleEndian.PutUint32(fixed[:], math.Float32bits(value))
			buf.Write(fixed[:])
		}
	}
	if terminator {
		n := pbEncodeVarint(tmp[:], uint64((16<<3)|0))
		buf.Write(tmp[:n])
		n = pbEncodeVarint(tmp[:], 1)
		buf.Write(tmp[:n])
	}

	return buf.Bytes()
}

// decodeUDPAudio parses a MumbleUDP.Audio protobuf message.
// Uses standard protobuf varint decoding, not Mumble's custom varint.
func decodeUDPAudio(data []byte) (session uint32, frameNumber uint64, opusData []byte, terminator bool, context uint32, position *[3]float32, volumeAdjustment float32) {
	var positionValues [3]float32
	positionCount := 0
	pos := 0
	for pos < len(data) {
		key, n := pbDecodeVarint(data[pos:])
		if n <= 0 {
			break
		}
		pos += n
		fieldNum := int(key >> 3)
		wireType := int(key & 0x7)

		switch wireType {
		case 0: // varint
			val, n := pbDecodeVarint(data[pos:])
			if n <= 0 {
				return
			}
			pos += n
			switch fieldNum {
			case 2:
				context = uint32(val)
			case 3:
				session = uint32(val)
			case 4:
				frameNumber = val
			case 16:
				terminator = val != 0
			}
		case 2: // length-delimited
			length, n := pbDecodeVarint(data[pos:])
			if n <= 0 {
				return
			}
			pos += n
			if length > uint64(len(data)-pos) {
				return
			}
			end := pos + int(length)
			if fieldNum == 5 {
				opusData = append(opusData[:0], data[pos:end]...)
			} else if fieldNum == 6 && length == 12 {
				for i := range positionValues {
					positionValues[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[pos+i*4:]))
				}
				positionCount = 3
			}
			pos = end
		case 1:
			if len(data)-pos < 8 {
				return
			}
			pos += 8
		case 5:
			if len(data)-pos < 4 {
				return
			}
			value := math.Float32frombits(binary.LittleEndian.Uint32(data[pos:]))
			if fieldNum == 6 && positionCount < len(positionValues) {
				positionValues[positionCount] = value
				positionCount++
			} else if fieldNum == 7 {
				volumeAdjustment = value
			}
			pos += 4
		default:
			return // invalid wire type
		}
	}
	if positionCount == len(positionValues) {
		position = &positionValues
	}
	return
}

// ---------------------------------------------------------------------------
// Mumble 1.5 native UDP crypto (AES-128-OCB with IV-prefix header).
//
// Wire format: [iv_byte(1)][tag(3)][ciphertext]
// Nonce is the full 16-byte IV, incremented per-packet.
// ---------------------------------------------------------------------------

const (
	udp15BlockSize  = 16
	udp15HeaderSize = 4 // 1 byte IV + 3 bytes tag
)

// cryptState15 implements Mumble 1.5 native UDP encryption.
type cryptState15 struct {
	mu          sync.Mutex
	key         [16]byte
	encryptIV   [16]byte
	decryptIV   [16]byte
	history     [256]byte // replay: history[iv_byte] == expected next byte
	initialized bool
}

// setup15 initializes 1.5-style crypto from CryptSetup.
// clientNonce → encryptIV, serverNonce → decryptIV.
func (cs *cryptState15) setup15(key, clientNonce, serverNonce []byte) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if len(key) != 16 || len(clientNonce) != 16 || len(serverNonce) != 16 {
		return errors.New("gumble: invalid crypto key/nonce")
	}
	copy(cs.key[:], key)
	copy(cs.encryptIV[:], clientNonce)
	copy(cs.decryptIV[:], serverNonce)
	cs.initialized = true

	if log.Enabled(log.LevelDebug) {
		log.Debug("cryptState15 setup complete: key_len=%d nonce_len=%d", len(key), len(clientNonce))
	}

	return nil
}

// encrypt15 encrypts plaintext for Mumble 1.5 native UDP.
// Returns [iv_byte(1)][tag(3)][ciphertext].
func (cs *cryptState15) encrypt15(plaintext []byte) ([]byte, error) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if !cs.initialized {
		return nil, errors.New("gumble: crypto not initialized")
	}

	// Increment IV (little-endian, byte 0 is LSB).
	advanceIV(cs.encryptIV[:])

	ciphertext, tag := ocb15Encrypt(cs.key[:], cs.encryptIV[:], plaintext)

	out := make([]byte, udp15HeaderSize+len(ciphertext))
	out[0] = cs.encryptIV[0]
	out[1] = tag[0]
	out[2] = tag[1]
	out[3] = tag[2]
	copy(out[4:], ciphertext)
	return out, nil
}

// decrypt15 decrypts a Mumble 1.5 native UDP packet.
// Matches wumble's decrypt: advances IV when decrypt_iv[0]+1 == iv_byte.
func (cs *cryptState15) decrypt15(packet []byte) ([]byte, error) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if !cs.initialized {
		return nil, errors.New("gumble: crypto not initialized")
	}
	if len(packet) < udp15HeaderSize {
		return nil, errors.New("gumble: packet too short")
	}

	ivByte := packet[0]
	expectedTag := packet[1:4]
	encrypted := packet[4:]

	savedIV := cs.decryptIV
	restore := false

	// Match wumble: if decrypt_iv[0] + 1 == iv_byte, advance and accept.
	next := cs.decryptIV[0] + 1
	if next == ivByte {
		if ivByte < cs.decryptIV[0] {
			advanceIV(cs.decryptIV[:])
		}
		cs.decryptIV[0] = ivByte
	} else {
		diff := int(ivByte) - int(cs.decryptIV[0])
		if diff < -128 {
			diff += 256
		} else if diff > 128 {
			diff -= 256
		}

		if ivByte < cs.decryptIV[0] && diff > -30 && diff < 0 {
			// Late packet.
			cs.decryptIV[0] = ivByte
			restore = true
		} else if ivByte > cs.decryptIV[0] && diff > -30 && diff < 0 {
			// Late packet (wrapped diff).
			cs.decryptIV[0] = ivByte
			backupIV(cs.decryptIV[:])
			restore = true
		} else if ivByte > cs.decryptIV[0] && diff > 0 {
			// We missed packets; move the low IV byte forward.
			cs.decryptIV[0] = ivByte
		} else if ivByte < cs.decryptIV[0] && diff > 0 {
			// We missed packets across a low-byte wrap. The IV's higher
			// bytes must advance even though the received low byte is set
			// below rather than incremented.
			advanceIVHighBytes(cs.decryptIV[:])
			cs.decryptIV[0] = ivByte
		} else {
			return nil, errors.New("gumble: OCB IV too far off")
		}

		// Replay check.
		if cs.history[ivByte] != 0 && cs.history[ivByte] == cs.decryptIV[1] {
			cs.decryptIV = savedIV
			return nil, errors.New("gumble: OCB replay detected")
		}
	}

	plaintext, tag, err := ocb15Decrypt(cs.key[:], cs.decryptIV[:], encrypted)
	if err != nil {
		cs.decryptIV = savedIV
		return nil, err
	}

	// Verify first 3 bytes of tag.
	if subtle.ConstantTimeCompare(tag[:3], expectedTag) != 1 {
		cs.decryptIV = savedIV
		return nil, errors.New("gumble: OCB authentication failed")
	}

	// Update replay history.
	cs.history[ivByte] = cs.decryptIV[1]

	if restore {
		cs.decryptIV = savedIV
	}

	return plaintext, nil
}

// advanceIV increments a 16-byte IV as a little-endian integer.
func advanceIV(iv []byte) {
	for i := 0; i < len(iv); i++ {
		iv[i]++
		if iv[i] != 0 {
			break
		}
	}
}

// advanceIVHighBytes advances all but the low IV byte as a little-endian integer.
func advanceIVHighBytes(iv []byte) {
	for i := 1; i < len(iv); i++ {
		iv[i]++
		if iv[i] != 0 {
			break
		}
	}
}

// backupIV decrements a 16-byte IV as a little-endian integer.
func backupIV(iv []byte) {
	for i := 0; i < len(iv); i++ {
		if iv[i] == 0 {
			iv[i] = 0xFF
		} else {
			iv[i]--
			break
		}
	}
}

// ---------------------------------------------------------------------------
// OCB variant for Mumble 1.5 native UDP.
// Matches the implementation in Wumble's crypt_state.cr.
// ---------------------------------------------------------------------------

// ocb15Encrypt encrypts with AES-128-OCB (no associated data).
// Returns ciphertext and 16-byte tag.
func ocb15Encrypt(key, nonce, plaintext []byte) (ciphertext, tag []byte) {
	block, _ := aes.NewCipher(key)

	// delta = AES_K(nonce)
	delta := make([]byte, 16)
	block.Encrypt(delta, nonce)

	checksum := make([]byte, 16)

	pos := 0
	remaining := len(plaintext)

	// Full blocks.
	for remaining > 16 {
		shift2inplace(delta)

		// Mitigate the XEX* forgery attack (eprint 2019/311), matching
		// Mumble's CryptStateOCB2 implementation.
		flipBit := remaining <= 32
		if flipBit {
			for _, b := range plaintext[pos : pos+15] {
				if b != 0 {
					flipBit = false
					break
				}
			}
		}
		xor16(checksum, checksum, plaintext[pos:pos+16])
		if flipBit {
			checksum[0] ^= 1
		}

		// C = delta XOR AES_K(delta XOR plaintext)
		tmp := make([]byte, 16)
		xorBytes(tmp, plaintext[pos:pos+16], delta)
		if flipBit {
			tmp[0] ^= 1
		}
		block.Encrypt(tmp, tmp)
		xorBytes(tmp, tmp, delta)

		ciphertext = append(ciphertext, tmp...)
		pos += 16
		remaining -= 16
	}

	// Final partial block.
	shift2inplace(delta)

	// pad = AES_K(temporary XOR delta) where temporary[15] = remaining*8
	tmp := make([]byte, 16)
	tmp[15] = byte(remaining * 8)
	xor16(tmp, tmp, delta)
	pad := make([]byte, 16)
	block.Encrypt(pad, tmp)

	// Cpartial = plaintext XOR pad (truncated)
	cpart := make([]byte, remaining)
	xorBytes(cpart, plaintext[pos:pos+remaining], pad[:remaining])
	ciphertext = append(ciphertext, cpart...)

	// checksum ^= (cpart || 0*) XOR pad
	csTemp := make([]byte, 16)
	copy(csTemp, cpart)
	xor16(csTemp, csTemp, pad)
	xor16(checksum, checksum, csTemp)

	// Tag = AES_K(3*delta XOR checksum)
	shift3inplace(delta)
	xor16(delta, delta, checksum)
	tag = make([]byte, 16)
	block.Encrypt(tag, delta)

	return ciphertext, tag
}

// ocb15Decrypt decrypts with AES-128-OCB (no associated data).
// Returns plaintext and 16-byte tag.
func ocb15Decrypt(key, nonce, ciphertext []byte) (plaintext, tag []byte, err error) {
	block, _ := aes.NewCipher(key)

	delta := make([]byte, 16)
	block.Encrypt(delta, nonce)

	checksum := make([]byte, 16)

	pos := 0
	remaining := len(ciphertext)

	// Full blocks.
	for remaining > 16 {
		shift2inplace(delta)

		// P = delta XOR AES_D_K(delta XOR ciphertext)
		tmp := make([]byte, 16)
		xorBytes(tmp, ciphertext[pos:pos+16], delta)
		block.Decrypt(tmp, tmp)
		xorBytes(tmp, tmp, delta)
		plaintext = append(plaintext, tmp...)

		xor16(checksum, checksum, tmp)
		pos += 16
		remaining -= 16
	}

	// Final partial block.
	shift2inplace(delta)

	tmp := make([]byte, 16)
	tmp[15] = byte(remaining * 8)
	xor16(tmp, tmp, delta)
	pad := make([]byte, 16)
	block.Encrypt(pad, tmp)

	// Ppartial = ciphertext XOR pad (truncated)
	ppart := make([]byte, remaining)
	xorBytes(ppart, ciphertext[pos:pos+remaining], pad[:remaining])
	plaintext = append(plaintext, ppart...)

	// checksum ^= (ciphertext_partial || 0*) XOR pad
	// This gives ciphertext XOR pad = plaintext, matching encrypt's checksum.
	csTemp := make([]byte, 16)
	copy(csTemp, ciphertext[pos:pos+remaining])
	xor16(csTemp, csTemp, pad)
	xor16(checksum, checksum, csTemp)

	// Reject the XEX* forgery pattern before authenticating the tag.
	matchesDelta := true
	for i := 0; i < 15; i++ {
		if csTemp[i] != delta[i] {
			matchesDelta = false
			break
		}
	}
	if matchesDelta {
		return nil, nil, errors.New("gumble: OCB XEX* forgery detected")
	}

	// Tag = AES_K(3*delta XOR checksum)
	shift3inplace(delta)
	xor16(delta, delta, checksum)
	tag = make([]byte, 16)
	block.Encrypt(tag, delta)

	return plaintext, tag, nil
}

// ---------------------------------------------------------------------------
// GF(2^128) helpers (same as crypt.go's doubleBlock, but in-place).
// ---------------------------------------------------------------------------

func shift2inplace(block []byte) {
	carry := (block[0] >> 7) & 1
	for i := 0; i < 15; i++ {
		block[i] = (block[i] << 1) | (block[i+1] >> 7)
	}
	block[15] = (block[15] << 1) ^ (carry * 0x87)
}

func shift3inplace(block []byte) {
	orig := make([]byte, 16)
	copy(orig, block)
	shift2inplace(block)
	xor16(block, block, orig)
}

func xor16(dst, a, b []byte) {
	dst[0] = a[0] ^ b[0]
	dst[1] = a[1] ^ b[1]
	dst[2] = a[2] ^ b[2]
	dst[3] = a[3] ^ b[3]
	dst[4] = a[4] ^ b[4]
	dst[5] = a[5] ^ b[5]
	dst[6] = a[6] ^ b[6]
	dst[7] = a[7] ^ b[7]
	dst[8] = a[8] ^ b[8]
	dst[9] = a[9] ^ b[9]
	dst[10] = a[10] ^ b[10]
	dst[11] = a[11] ^ b[11]
	dst[12] = a[12] ^ b[12]
	dst[13] = a[13] ^ b[13]
	dst[14] = a[14] ^ b[14]
	dst[15] = a[15] ^ b[15]
}

func xorBytes(dst, a, b []byte) {
	for i := 0; i < len(dst); i++ {
		dst[i] = a[i] ^ b[i]
	}
}

// setUDP15Crypto installs per-client 1.5 UDP crypto state from CryptSetup.
func (c *Client) setUDP15Crypto(key, clientNonce, serverNonce []byte) error {
	outbound := &cryptState15{}
	if err := outbound.setup15(key, clientNonce, serverNonce); err != nil {
		return err
	}
	inbound := &cryptState15{}
	// Server-to-client packets use serverNonce as decryptIV.
	if err := inbound.setup15(key, clientNonce, serverNonce); err != nil {
		return err
	}
	c.udpWriteMu.Lock()
	c.udpMu.Lock()
	c.udpCryptoOut = outbound
	c.udpCryptoIn = inbound
	c.udpFrameNumber = 0
	c.udpMu.Unlock()
	c.udpWriteMu.Unlock()
	log.Info("Mumble 1.5 native UDP crypto initialized")
	return nil
}

// encodeLegacyUDPAudio builds the pre-1.5 UDPVoice packet payload.
func encodeLegacyUDPAudio(format, target byte, sequence int64, data []byte, final bool, X, Y, Z *float32) []byte {
	var header [1 + varint.MaxVarintLen*2]byte
	header[0] = format<<5 | target
	n := varint.Encode(header[1:], sequence)
	length := int64(len(data))
	if final {
		length |= 0x2000
	}
	m := varint.Encode(header[1+n:], length)
	payload := append([]byte(nil), header[:1+n+m]...)
	payload = append(payload, data...)
	if X != nil && Y != nil && Z != nil {
		for _, value := range []float32{*X, *Y, *Z} {
			var fixed [4]byte
			binary.LittleEndian.PutUint32(fixed[:], math.Float32bits(value))
			payload = append(payload, fixed[:]...)
		}
	}
	return payload
}

// ---------------------------------------------------------------------------
// WriteAudioUDP15 writes encrypted UDP audio in the negotiated payload format.
// Returns true if sent, false if TCP should be used.
func (c *Client) WriteAudioUDP15(format byte, target uint32, sequence int64, data []byte, final bool, X, Y, Z *float32) (bool, error) {
	// Encryption and socket writes must remain ordered: otherwise a later
	// packet can reach the server before the packet with the preceding IV.
	c.udpWriteMu.Lock()
	defer c.udpWriteMu.Unlock()
	c.udpMu.Lock()
	cs, udpConn := c.udpCryptoOut, c.udpConn
	frameNum := c.udpFrameNumber
	protobuf := c.udpProtobuf
	if protobuf {
		c.udpFrameNumber++
	}
	c.udpMu.Unlock()
	if cs == nil || udpConn == nil {
		return false, nil
	}

	var payload []byte
	if protobuf {
		payload = append([]byte{0x00}, encodeUDPAudio(target, frameNum, data, final, X, Y, Z)...)
	} else {
		payload = encodeLegacyUDPAudio(format, byte(target), sequence, data, final, X, Y, Z)
	}
	encrypted, err := cs.encrypt15(payload)
	if err != nil {
		log.Error("UDP15 encrypt failed: %v", err)
		return false, err
	}

	if log.Enabled(log.LevelDebug) && (frameNum < 3 || frameNum%1000 == 0 || final) {
		log.Debug("UDP15 send: frame=%d opus_len=%d enc_len=%d final=%v",
			frameNum, len(data), len(encrypted), final)
	}

	_, err = udpConn.Write(encrypted)
	if err != nil {
		log.Error("UDP15 send write failed: %v", err)
		return false, err
	}
	return true, nil
}

// HandleUDPPacket15 processes an incoming Mumble 1.5 native UDP packet.
func (c *Client) HandleUDPPacket15(packet []byte, pktNum uint64) {
	if len(packet) < udp15HeaderSize {
		log.Warn("UDP15 #%d: packet too short (%d bytes)", pktNum, len(packet))
		return
	}

	if !c.udpFirstRecv.Swap(true) && log.Enabled(log.LevelDebug) {
		log.Debug("UDP15 #%d: first packet received (%d bytes)", pktNum, len(packet))
	}

	c.udpMu.RLock()
	cs := c.udpCryptoIn
	c.udpMu.RUnlock()
	if cs == nil {
		log.Warn("UDP15 #%d: crypto not initialized", pktNum)
		return
	}

	plaintext, err := cs.decrypt15(packet)
	if err != nil {
		log.Warn("UDP15 #%d: decrypt failed: %v", pktNum, err)
		return
	}

	if log.Enabled(log.LevelDebug) && (pktNum <= 3 || pktNum%1000 == 0) {
		log.Debug("UDP15 #%d: decrypt OK, plaintext_len=%d", pktNum, len(plaintext))
	}
	c.markUDPActive()

	// Check type byte (0x00 = Audio, 0x01 = Ping)
	if len(plaintext) < 1 {
		return
	}
	msgType := plaintext[0]
	plaintext = plaintext[1:]

	if msgType == 0x01 {
		// Ping response — just a timestamp, no action needed.
		log.Info("UDP15 #%d: ping response, ignoring", pktNum)
		return
	}

	// MumbleUDP.Audio protobuf format (1.5 native).
	if msgType == 0x00 {
		session, frameNum, opusData, terminator, context, position, volumeAdjustment := decodeUDPAudio(plaintext)
		c.dispatchOpus15(pktNum, session, int64(frameNum), opusData, terminator, context, position, volumeAdjustment)
		return
	}

	// Legacy UDPVoiceOpus format: type byte has bits 5-7 = 4.
	if (msgType >> 5) == 4 {
		c.handleLegacyUDPVoice(pktNum, plaintext)
		return
	}

	log.Warn("UDP15 #%d: unknown message type 0x%02x", pktNum, msgType)
}

// markUDPActive switches outgoing audio to UDP only after authentication has
// proved that packets can return through the network path.
func (c *Client) markUDPActive() {
	c.udpMu.Lock()
	c.udpActive = true
	c.udpMu.Unlock()
}

// dispatchOpus15 processes a decoded MumbleUDP.Audio frame and dispatches
// the decoded PCM to audio listeners.
func (c *Client) dispatchOpus15(pktNum uint64, session uint32, frameNum int64, opusData []byte, terminator bool, context uint32, position *[3]float32, volumeAdjustment float32) {
	// This runs on the UDP reader independently of TCP state handlers.
	c.volatile.RLock()
	defer c.volatile.RUnlock()
	if len(opusData) == 0 && !terminator {
		log.Info("UDP15 #%d: no opus data (session=%d frame=%d), skipping", pktNum, session, frameNum)
		return
	}

	user := c.Users[session]
	if user == nil {
		log.Warn("UDP15 #%d: unknown session %d", pktNum, session)
		return
	}

	decoder := user.decoder
	if decoder == nil {
		codec := c.audioCodec
		if codec == nil {
			log.Warn("UDP15 #%d: no audio codec", pktNum)
			return
		}
		decoder = codec.NewDecoder()
		user.decoder = decoder
		log.Info("UDP15 #%d: new decoder for %s", pktNum, user.Name)
	}

	if terminator && len(opusData) == 0 {
		decoder.Reset()
		user.audioSequenceValid = false
		user.audioFrameStep = 0
		// The audio stream remains open between talk bursts. Deliver the
		// terminator so listeners can reset their own packet ordering state.
		c.dispatchAudio(user, &AudioPacket{Client: c, Sender: user, Terminator: true})
		log.Info("UDP15 #%d: terminator for %s, decoder reset", pktNum, user.Name)
		return
	}

	if len(opusData) == 0 {
		return
	}

	c.decodeAndDispatch(pktNum, user, decoder, frameNum, opusData, terminator, context, position, volumeAdjustment)
}

// handleLegacyUDPVoice parses the legacy UDPVoice format (type byte 0x80)
// inside a 1.5-decrypted payload.
func (c *Client) handleLegacyUDPVoice(pktNum uint64, data []byte) {
	pos := 0

	// Session varint.
	session, n := varint.Decode(data[pos:])
	if n <= 0 {
		log.Warn("UDP15 #%d: legacy session varint decode failed", pktNum)
		return
	}
	pos += n

	// Sequence varint.
	seq, n := varint.Decode(data[pos:])
	if n <= 0 {
		log.Warn("UDP15 #%d: legacy seq varint decode failed", pktNum)
		return
	}
	pos += n

	// Length varint (bit 13 = terminator).
	length, n := varint.Decode(data[pos:])
	if n <= 0 {
		log.Warn("UDP15 #%d: legacy length varint decode failed", pktNum)
		return
	}
	pos += n

	terminator := (length & 0x2000) != 0
	audioLen := int(length &^ 0x2000)
	if audioLen > len(data)-pos {
		log.Warn("UDP15 #%d: legacy audio length %d > remaining %d",
			pktNum, audioLen, len(data)-pos)
		return
	}

	opusData := data[pos : pos+audioLen]

	log.Info("UDP15 #%d: legacy voice session=%d seq=%d opus_len=%d term=%v",
		pktNum, session, seq, len(opusData), terminator)

	c.dispatchOpus15(pktNum, uint32(session), seq, opusData, terminator, 0, nil, 0)
}

// decodeAndDispatch decodes an Opus frame and dispatches PCM to audio listeners.
func (c *Client) decodeAndDispatch(pktNum uint64, user *User, decoder AudioDecoder, frameNum int64, opusData []byte, terminator bool, context uint32, position *[3]float32, volumeAdjustment float32) {
	// Frame numbers are timestamps in 10 ms units, not packet counters. For
	// example, a standard 20 ms Opus packet advances its frame number by two.
	// Only generate PLC for complete missing packets; treating every timestamp
	// unit as a packet doubles playout and eventually exhausts OpenAL buffers.
	if user.audioSequenceValid {
		gap := frameNum - user.audioSequence
		frameStep := user.audioFrameStep
		if frameStep < 1 {
			frameStep = 1
		}
		if gap > frameStep && gap < 100 {
			if missing := missingAudioPackets(gap, frameStep); missing > 0 {
				log.Info("UDP15 #%d: audio gap for %s: %d -> %d (loss=%d), generating PLC",
					pktNum, user.Name, user.audioSequence, frameNum, missing)
				for i := int64(1); i <= missing; i++ {
					c.dispatchPLC15(user, decoder, user.audioSequence+i*frameStep)
				}
			}
		} else if gap < 0 && gap > -100 {
			log.Info("UDP15 #%d: seq reorder for %s: %d -> %d, resetting decoder",
				pktNum, user.Name, user.audioSequence, frameNum)
			decoder.Reset()
		} else if gap == 0 {
			log.Info("UDP15 #%d: duplicate seq=%d for %s", pktNum, frameNum, user.Name)
			return
		}
	}

	pcm, err := decoder.Decode(opusData, AudioMaximumFrameSize)
	if err != nil {
		log.Warn("UDP15 #%d: Opus decode failed for %s: %v", pktNum, user.Name, err)
		decoder.Reset()
		return
	}

	if log.Enabled(log.LevelDebug) && (pktNum <= 3 || pktNum%1000 == 0) {
		log.Debug("UDP15 #%d: Opus OK for %s, pcm_samples=%d", pktNum, user.Name, len(pcm))
	}
	user.audioSequence = frameNum
	user.audioSequenceValid = true
	user.audioFrameStep = audioFrameStep(len(pcm))

	event := AudioPacket{
		Client:           c,
		Sender:           user,
		Target:           &VoiceTarget{ID: context},
		Sequence:         frameNum,
		AudioBuffer:      AudioBuffer(pcm),
		VolumeAdjustment: volumeAdjustment,
	}
	if position != nil {
		event.HasPosition = true
		event.X, event.Y, event.Z = position[0], position[1], position[2]
	}
	c.dispatchAudio(user, &event)
	if terminator {
		decoder.Reset()
		user.audioSequenceValid = false
		user.audioFrameStep = 0
		c.dispatchAudio(user, &AudioPacket{Client: c, Sender: user, Terminator: true})
	}
}

// missingAudioPackets returns the number of whole packets absent from a
// timestamp gap. A non-integral gap cannot reliably identify a missing packet.
func missingAudioPackets(gap, frameStep int64) int64 {
	if frameStep < 1 || gap <= frameStep || gap%frameStep != 0 {
		return 0
	}
	return gap/frameStep - 1
}

// audioFrameStep converts interleaved stereo PCM length to Mumble's 10 ms
// frame-number units.
func audioFrameStep(samples int) int64 {
	frames := samples / AudioChannels
	step := int64(frames / AudioDefaultFrameSize)
	if step < 1 {
		return 1
	}
	return step
}

// dispatchPLC15 generates a Packet Loss Concealment frame for 1.5 UDP.
func (c *Client) dispatchPLC15(user *User, decoder AudioDecoder, sequence int64) {
	pcm, err := decoder.Decode(nil, AudioMaximumFrameSize)
	if err != nil {
		decoder.Reset()
		return
	}
	event := AudioPacket{
		Client:      c,
		Sender:      user,
		Target:      &VoiceTarget{ID: 0},
		Sequence:    sequence,
		AudioBuffer: AudioBuffer(pcm),
	}
	c.dispatchAudio(user, &event)
}
