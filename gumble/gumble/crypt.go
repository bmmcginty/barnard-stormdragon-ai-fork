package gumble

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/subtle"
	"encoding/binary"
	"errors"
	"sync"

	"git.stormux.org/storm/barnard/gumble/gumble/MumbleProto"
	"git.stormux.org/storm/barnard/log"
	"google.golang.org/protobuf/proto"
)

// ocbEncrypt performs OCB-AES128 encryption.
// nonce is 1-15 bytes. Returns ciphertext || 16-byte tag.
func ocbEncrypt(key, nonce, plaintext, ad []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return ocbCrypt(block, nonce, plaintext, ad, true)
}

// ocbDecrypt performs OCB-AES128 decryption. ciphertext includes the
// 16-byte tag as its last 16 bytes.
func ocbDecrypt(key, nonce, ciphertext, ad []byte) ([]byte, error) {
	if len(ciphertext) < 16 {
		return nil, errors.New("gumble: ciphertext too short for OCB tag")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return ocbCrypt(block, nonce, ciphertext, ad, false)
}

func ocbCrypt(block cipher.Block, nonce, data, ad []byte, encrypt bool) ([]byte, error) {
	blockSize := block.BlockSize() // 16
	if len(nonce) < 1 || len(nonce) > blockSize-1 {
		return nil, errors.New("gumble: OCB nonce must be 1-15 bytes")
	}

	// --- Initial offset from nonce ---
	// Pad nonce to 16 bytes, encrypt with AES, mask low bits.
	var padded [16]byte
	copy(padded[:], nonce)
	var offset [16]byte
	block.Encrypt(offset[:], padded[:])

	// Clear low bits based on nonce length.
	// For 12-byte nonce: bottom = 128-96 = 32 bits to clear.
	// Clear the last 4 bytes (offset[12..15]).
	bottom := blockSize*8 - len(nonce)*8
	if bottom < 128 {
		bytesToClear := bottom / 8
		bitsToClear := bottom % 8
		for i := blockSize - bytesToClear; i < blockSize; i++ {
			offset[i] = 0
		}
		if bitsToClear > 0 {
			mask := byte(0xFF) >> bitsToClear
			offset[blockSize-bytesToClear-1] &= mask
		}
	}

	// --- L_* = E_K(0^128), the base for doubling ---
	var Lstar [16]byte
	block.Encrypt(Lstar[:], make([]byte, 16))

	// Helper: L_ntz(i) = Lstar doubled ntz(i) times.
	Lntz := func(i int) [16]byte {
		if i == 0 {
			return Lstar
		}
		n := 0
		v := i
		for v&1 == 0 {
			v >>= 1
			n++
		}
		l := Lstar
		for j := 0; j < n; j++ {
			l = doubleBlock(l)
		}
		return l
	}

	// --- Data blocks ---
	tagLen := blockSize
	var m int
	if encrypt {
		m = (len(data) + blockSize - 1) / blockSize
	} else {
		m = (len(data) - tagLen + blockSize - 1) / blockSize
	}

	var checksum [16]byte
	out := make([]byte, 0, len(data))

	for i := 1; i <= m; i++ {
		l := Lntz(i)
		for j := 0; j < blockSize; j++ {
			offset[j] ^= l[j]
		}

		if encrypt {
			if i == m {
				lastLen := len(data) - (i-1)*blockSize
				var pad [16]byte
				block.Encrypt(pad[:], offset[:])
				for j := 0; j < lastLen; j++ {
					out = append(out, data[(i-1)*blockSize+j]^pad[j])
				}
				// checksum: plaintext zero-padded to 16 bytes
				for j := 0; j < lastLen; j++ {
					checksum[j] ^= data[(i-1)*blockSize+j]
				}
			} else {
				for j := 0; j < blockSize; j++ {
					checksum[j] ^= data[(i-1)*blockSize+j]
				}
				var tmp [16]byte
				for j := 0; j < blockSize; j++ {
					tmp[j] = offset[j] ^ data[(i-1)*blockSize+j]
				}
				block.Encrypt(tmp[:], tmp[:])
				for j := 0; j < blockSize; j++ {
					tmp[j] ^= offset[j]
				}
				out = append(out, tmp[:]...)
			}
		} else {
			if i == m {
				lastLen := len(data) - tagLen - (i-1)*blockSize
				var pad [16]byte
				block.Encrypt(pad[:], offset[:])
				for j := 0; j < lastLen; j++ {
					out = append(out, data[(i-1)*blockSize+j]^pad[j])
				}
				for j := 0; j < lastLen; j++ {
					checksum[j] ^= out[len(out)-lastLen+j]
				}
			} else {
				var tmp [16]byte
				for j := 0; j < blockSize; j++ {
					tmp[j] = offset[j] ^ data[(i-1)*blockSize+j]
				}
				block.Decrypt(tmp[:], tmp[:])
				for j := 0; j < blockSize; j++ {
					tmp[j] ^= offset[j]
				}
				out = append(out, tmp[:]...)
				for j := 0; j < blockSize; j++ {
					checksum[j] ^= tmp[j]
				}
			}
		}
	}

	// --- Process associated data ---
	var adOffset [16]byte // starts at 0
	var adSum [16]byte
	adIdx := 1
	for len(ad) > 0 {
		// Update AD offset: Δ = Δ ⊕ L_ntz(adIdx)
		l := Lntz(adIdx)
		for j := 0; j < blockSize; j++ {
			adOffset[j] ^= l[j]
		}

		var adBlock [16]byte
		if len(ad) >= blockSize {
			copy(adBlock[:], ad[:blockSize])
			ad = ad[blockSize:]
		} else {
			copy(adBlock[:], ad)
			adBlock[len(ad)] = 0x80
			ad = nil
		}
		for j := 0; j < blockSize; j++ {
			adBlock[j] ^= adOffset[j]
		}
		block.Encrypt(adBlock[:], adBlock[:])
		for j := 0; j < blockSize; j++ {
			adSum[j] ^= adBlock[j]
		}
		adIdx++
	}

	// --- Tag = E_K(checksum XOR offset) XOR adSum ---
	for j := 0; j < blockSize; j++ {
		offset[j] ^= checksum[j]
	}
	block.Encrypt(offset[:], offset[:])
	for j := 0; j < blockSize; j++ {
		offset[j] ^= adSum[j]
	}

	if encrypt {
		out = append(out, offset[:tagLen]...)
	} else {
		tag := data[len(data)-tagLen:]
		if subtle.ConstantTimeCompare(tag, offset[:tagLen]) != 1 {
			return nil, errors.New("gumble: OCB authentication failed")
		}
	}
	return out, nil
}

// doubleBlock multiplies a 128-bit block by 2 in GF(2^128).
func doubleBlock(b [16]byte) [16]byte {
	var out [16]byte
	carry := (b[0] >> 7) & 1
	for i := 0; i < 15; i++ {
		out[i] = (b[i] << 1) | (b[i+1] >> 7)
	}
	out[15] = (b[15] << 1) ^ (carry * 0x87)
	return out
}

// --- Mumble CryptSetup and UDP encryption support ---

type cryptState struct {
	mu          sync.Mutex
	key         [16]byte
	nonce       [12]byte // derived from IV
	cipher      cipher.Block
	counter     uint32
	initialized bool
}

func (cs *cryptState) setup(key, iv []byte) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if len(key) != 16 {
		return errors.New("gumble: crypt key must be 16 bytes")
	}
	copy(cs.key[:], key)

	block, err := aes.NewCipher(cs.key[:])
	if err != nil {
		return err
	}
	cs.cipher = block

	// Mumble nonce: AES(key, IV)[0:4] || 0x0000000000000000
	var encIV [16]byte
	copy(encIV[:], iv)
	block.Encrypt(encIV[:], encIV[:])
	copy(cs.nonce[:4], encIV[:4])

	cs.initialized = true

	if log.Enabled(log.LevelDebug) {
		log.Debug("cryptState setup complete: key_len=%d iv_len=%d", len(key), len(iv))
	}

	return nil
}

// nonceForPacket returns the 12-byte OCB nonce for a given packet counter.
func (cs *cryptState) nonceForPacket(counter uint32) [12]byte {
	var n [12]byte
	copy(n[:], cs.nonce[:])
	prefix := binary.BigEndian.Uint32(n[0:4])
	binary.BigEndian.PutUint32(n[0:4], prefix^counter)
	return n
}

func (cs *cryptState) isInitialized() bool {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	return cs.initialized
}

func (cs *cryptState) encrypt(counter uint32, plaintext []byte) ([]byte, error) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if !cs.initialized {
		return plaintext, nil
	}
	nonce := cs.nonceForPacket(counter)
	return ocbEncrypt(cs.key[:], nonce[:], plaintext, nil)
}

func (cs *cryptState) decrypt(counter uint32, ciphertext []byte) ([]byte, error) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if !cs.initialized {
		return ciphertext, nil
	}
	nonce := cs.nonceForPacket(counter)
	return ocbDecrypt(cs.key[:], nonce[:], ciphertext, nil)
}

// handleCryptSetup processes the CryptSetup message from the server.
func (c *Client) handleCryptSetup(buffer []byte) error {
	var packet MumbleProto.CryptSetup
	if err := proto.Unmarshal(buffer, &packet); err != nil {
		return err
	}

	c.volatile.Lock()
	defer c.volatile.Unlock()

	if packet.Key != nil && packet.ClientNonce != nil && packet.ServerNonce != nil {
		wasInit := c.cryptOut.isInitialized()
		c.cryptOut.setup(packet.Key, packet.ClientNonce)
		c.cryptIn.setup(packet.Key, packet.ServerNonce)

		// Also set up per-client Mumble 1.5 native UDP crypto.
		if err := c.setUDP15Crypto(packet.Key, packet.ClientNonce, packet.ServerNonce); err != nil {
			return err
		}

		if wasInit {
			log.Debug("CryptSetup updated (key rotation)")
		} else {
			log.Info("received CryptSetup: key_len=%d client_nonce_len=%d server_nonce_len=%d",
				len(packet.Key), len(packet.ClientNonce), len(packet.ServerNonce))
		}
	} else if !c.cryptOut.isInitialized() {
		// Only log incomplete once before crypto is set up
		log.Debug("received CryptSetup with incomplete fields, waiting for full key exchange")
	}

	cryptoReady := c.cryptOut.isInitialized()
	c.udpMu.Lock()
	udpReady := c.udpCryptoOut != nil
	startUDP := cryptoReady && c.udpConn != nil && !c.udpStarted && udpReady
	if startUDP {
		// Keep TCP tunnelling enabled until an authenticated UDP packet proves
		// that the inbound path works.
		c.udpStarted = true
	}
	noUDPConn := c.udpConn == nil
	c.udpMu.Unlock()
	if startUDP {
		log.Info("UDP crypto ready (1.5 native), starting UDP reader and pinger")
		go c.udpReadRoutine()
		go c.udpPingRoutine()
	} else if cryptoReady && noUDPConn {
		log.Warn("crypto ready but no UDP socket — audio will use TCP tunnel")
	}

	return nil
}
