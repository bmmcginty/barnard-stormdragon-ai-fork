package gumble

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"math"
	"testing"
)

// Test IV increment matches wumble's little-endian behavior.
func TestAdvanceIV(t *testing.T) {
	iv := []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}

	advanceIV(iv)
	if iv[0] != 0x01 {
		t.Fatalf("after increment 1, iv[0]=%02x, want 01", iv[0])
	}

	for i := 0; i < 254; i++ {
		advanceIV(iv)
	}
	if iv[0] != 0xFF || iv[1] != 0x00 {
		t.Fatalf("after 255 increments, iv[0]=%02x iv[1]=%02x, want FF 00", iv[0], iv[1])
	}

	advanceIV(iv)
	if iv[0] != 0x00 || iv[1] != 0x01 {
		t.Fatalf("after 256 increments, iv[0]=%02x iv[1]=%02x, want 00 01", iv[0], iv[1])
	}
}

// Regression coverage for the native IV carry path at the 255->256 wrap.
func TestCryptState15DecryptsAfterMissedPackets(t *testing.T) {
	key := mustDecodeHex("93360b0f86a926c4561563469026eb94")
	nonce := mustDecodeHex("10000000000000000000000000000000")
	out, in := &cryptState15{}, &cryptState15{}
	if err := out.setup15(key, nonce, nonce); err != nil {
		t.Fatal(err)
	}
	if err := in.setup15(key, nonce, nonce); err != nil {
		t.Fatal(err)
	}

	first, err := out.encrypt15([]byte("first"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := in.decrypt15(first); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if _, err := out.encrypt15([]byte("dropped")); err != nil {
			t.Fatal(err)
		}
	}
	last, err := out.encrypt15([]byte("after loss"))
	if err != nil {
		t.Fatal(err)
	}
	plain, err := in.decrypt15(last)
	if err != nil || !bytes.Equal(plain, []byte("after loss")) {
		t.Fatalf("decrypt after missed packets = %q, %v", plain, err)
	}
}

func TestCryptState15DecryptsAfterMissedPacketsAcrossIVByteWrap(t *testing.T) {
	key := mustDecodeHex("93360b0f86a926c4561563469026eb94")
	nonce := mustDecodeHex("fa000000000000000000000000000000")
	out, in := &cryptState15{}, &cryptState15{}
	if err := out.setup15(key, nonce, nonce); err != nil {
		t.Fatal(err)
	}
	if err := in.setup15(key, nonce, nonce); err != nil {
		t.Fatal(err)
	}

	first, err := out.encrypt15([]byte("before wrap"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := in.decrypt15(first); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 6; i++ {
		if _, err := out.encrypt15([]byte("dropped")); err != nil {
			t.Fatal(err)
		}
	}
	last, err := out.encrypt15([]byte("after wrap"))
	if err != nil {
		t.Fatal(err)
	}
	plain, err := in.decrypt15(last)
	if err != nil || !bytes.Equal(plain, []byte("after wrap")) {
		t.Fatalf("decrypt after missed packets across IV wrap = %q, %v", plain, err)
	}
	if in.decryptIV[0] != 2 || in.decryptIV[1] != 1 {
		t.Fatalf("unexpected IV after wrapped loss: %x", in.decryptIV[:2])
	}
}

func TestCryptState15DecryptsAcrossIVByteWrap(t *testing.T) {
	key := mustDecodeHex("93360b0f86a926c4561563469026eb94")
	clientNonce := mustDecodeHex("ff000000000000000000000000000000")
	serverNonce := mustDecodeHex("ff000000000000000000000000000000")
	out, in := &cryptState15{}, &cryptState15{}
	if err := out.setup15(key, clientNonce, serverNonce); err != nil {
		t.Fatal(err)
	}
	if err := in.setup15(key, clientNonce, serverNonce); err != nil {
		t.Fatal(err)
	}
	for _, payload := range [][]byte{[]byte("wrap-1"), []byte("wrap-2")} {
		packet, err := out.encrypt15(payload)
		if err != nil {
			t.Fatal(err)
		}
		plain, err := in.decrypt15(packet)
		if err != nil || !bytes.Equal(plain, payload) {
			t.Fatalf("wrap decrypt %q: %v", plain, err)
		}
	}
	if in.decryptIV[0] != 1 || in.decryptIV[1] != 1 {
		t.Fatalf("unexpected wrapped IV %x", in.decryptIV[:2])
	}
}

// Test OCB round-trip: encrypt then decrypt should recover plaintext.
func TestOCB15RoundTrip(t *testing.T) {
	key := mustDecodeHex("000102030405060708090a0b0c0d0e0f")
	nonce := mustDecodeHex("000102030405060708090a0b0c0d0e0f")

	tests := []struct {
		name      string
		plaintext []byte
	}{
		{"empty", []byte{}},
		{"1 byte", []byte{0x41}},
		{"15 bytes", []byte("hello world 1234")},              // 15
		{"16 bytes", []byte("hello world 12345")},             // exactly 1 block
		{"17 bytes", []byte("hello world 123456")},            // 1 full + 1 partial
		{"32 bytes", []byte("hello world 12345678901234567")}, // exactly 2 blocks
		{"33 bytes", []byte("hello world 123456789012345678")},
		{"100 bytes", bytes.Repeat([]byte{0x41}, 100)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ct, tag := ocb15Encrypt(key, nonce, tt.plaintext)
			pt, tag2, err := ocb15Decrypt(key, nonce, ct)

			if err != nil {
				t.Fatalf("decrypt error: %v", err)
			}
			if !bytes.Equal(pt, tt.plaintext) {
				t.Fatalf("round-trip failed:\n  input:  %s\n  output: %s",
					hex.EncodeToString(tt.plaintext),
					hex.EncodeToString(pt))
			}
			if !bytes.Equal(tag, tag2) {
				t.Fatalf("tag mismatch:\n  encrypt tag: %s\n  decrypt tag: %s",
					hex.EncodeToString(tag),
					hex.EncodeToString(tag2))
			}
		})
	}
}

// Test full 1.5 crypto pipeline: setup -> encrypt -> decrypt.
func TestCryptState15RoundTrip(t *testing.T) {
	key := mustDecodeHex("93360b0f86a926c4561563469026eb94")
	clientNonce := mustDecodeHex("d4e53c00a2f6512a61cbe8540eba6314")
	serverNonce := mustDecodeHex("1463352a4d2375a2695ae0800b22d71d")

	csClient := &cryptState15{}
	if err := csClient.setup15(key, clientNonce, serverNonce); err != nil {
		t.Fatalf("client setup: %v", err)
	}

	csServer := &cryptState15{}
	if err := csServer.setup15(key, serverNonce, clientNonce); err != nil {
		t.Fatalf("server setup: %v", err)
	}

	plaintext := []byte("test audio frame")

	// Encrypt with client state.
	encrypted, err := csClient.encrypt15(plaintext)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	t.Logf("encrypted len=%d hex=%s", len(encrypted), hex.EncodeToString(encrypted))

	// Decrypt with server state.
	decrypted, err := csServer.decrypt15(encrypted)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}

	if !bytes.Equal(decrypted, plaintext) {
		t.Fatalf("round-trip failed:\n  input:  %s\n  output: %s",
			hex.EncodeToString(plaintext),
			hex.EncodeToString(decrypted))
	}

	t.Logf("Client IV after  encrypt: %s", hex.EncodeToString(csClient.encryptIV[:]))
	t.Logf("Server IV before decrypt: %s", hex.EncodeToString(csServer.decryptIV[:]))

	// Second packet.
	plaintext2 := []byte("second audio frame")
	encrypted2, _ := csClient.encrypt15(plaintext2)
	decrypted2, err := csServer.decrypt15(encrypted2)
	if err != nil {
		t.Fatalf("decrypt2: %v", err)
	}
	if !bytes.Equal(decrypted2, plaintext2) {
		t.Fatalf("round-trip 2 failed")
	}

	t.Logf("Client IV after 2 encrypts: %s", hex.EncodeToString(csClient.encryptIV[:]))
	t.Logf("Server IV after 2 decrypts: %s", hex.EncodeToString(csServer.decryptIV[:]))
}

// Regression coverage for the native UDP replay window: a captured packet
// must not be accepted twice after its IV byte has entered history.
func TestCryptState15RejectsReplay(t *testing.T) {
	key := mustDecodeHex("93360b0f86a926c4561563469026eb94")
	clientNonce := mustDecodeHex("d4e53c00a2f6512a61cbe8540eba6314")
	serverNonce := mustDecodeHex("1463352a4d2375a2695ae0800b22d71d")
	out, in := &cryptState15{}, &cryptState15{}
	if err := out.setup15(key, clientNonce, serverNonce); err != nil {
		t.Fatal(err)
	}
	if err := in.setup15(key, serverNonce, clientNonce); err != nil {
		t.Fatal(err)
	}
	packet, err := out.encrypt15([]byte("captured frame"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := in.decrypt15(packet); err != nil {
		t.Fatal(err)
	}
	if _, err := in.decrypt15(packet); err == nil {
		t.Fatal("replayed UDP packet was accepted")
	}
}

// Test protobuf encode/decode round-trip.
func TestUDPAudioProtobuf(t *testing.T) {
	tests := []struct {
		session     uint32
		frameNumber uint32
		opusData    []byte
		terminator  bool
	}{
		{0, 42, []byte{0x01, 0x02, 0x03}, false},
		{123, 0, []byte{}, true},
		{0, 99, []byte{0xFF}, false},
	}

	for _, tt := range tests {
		encoded := encodeUDPAudio(uint32(tt.session), uint64(tt.frameNumber), tt.opusData, tt.terminator, nil, nil, nil)
		session, frameNum, opusData, terminator, _, _, _ := decodeUDPAudio(encoded)

		if session != 0 {
			t.Errorf("outbound packet unexpectedly contains session %d", session)
		}
		if frameNum != uint64(tt.frameNumber) {
			t.Errorf("frameNumber: got %d, want %d", frameNum, tt.frameNumber)
		}
		if !bytes.Equal(opusData, tt.opusData) {
			t.Errorf("opusData mismatch: got %x, want %x", opusData, tt.opusData)
		}
		if terminator != tt.terminator {
			t.Errorf("terminator: got %v, want %v", terminator, tt.terminator)
		}
	}
}

// Reference wire vector from the MumbleUDP.Audio protobuf layout. This guards
// field numbers, standard-varint framing, terminators, and position encoding.
func TestUDPAudioProtobufReferenceVector(t *testing.T) {
	x, y, z := float32(1), float32(2), float32(3)
	got := encodeUDPAudio(2, 300, []byte{0xaa, 0xbb}, true, &x, &y, &z)
	want := mustDecodeHex("080220ac022a02aabb320c0000803f0000004000004040800101")
	if !bytes.Equal(got, want) {
		t.Fatalf("wire vector = %x, want %x", got, want)
	}
}

func TestAudioFrameTimestampGaps(t *testing.T) {
	tests := []struct {
		name      string
		gap, step int64
		want      int64
	}{
		{"consecutive 10 ms packets", 1, 1, 0},
		{"consecutive 20 ms packets", 2, 2, 0},
		{"one missing 20 ms packet", 4, 2, 1},
		{"two missing 20 ms packets", 6, 2, 2},
		{"non-integral timestamp gap", 3, 2, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := missingAudioPackets(tt.gap, tt.step); got != tt.want {
				t.Fatalf("missingAudioPackets(%d, %d) = %d, want %d", tt.gap, tt.step, got, tt.want)
			}
		})
	}

	if got := audioFrameStep(1920); got != 2 {
		t.Fatalf("audioFrameStep(1920) = %d, want 2", got)
	}
}

func TestUDPAudioProtobufIncomingFields(t *testing.T) {
	var packet bytes.Buffer
	writeVarint := func(v uint64) {
		var buf [10]byte
		n := pbEncodeVarint(buf[:], v)
		packet.Write(buf[:n])
	}
	writeVarint(2<<3 | 0) // context
	writeVarint(3)
	writeVarint(3<<3 | 0) // sender_session
	writeVarint(123)
	writeVarint(4<<3 | 0) // frame_number
	writeVarint(1 << 32)
	writeVarint(5<<3 | 2) // opus_data
	writeVarint(2)
	packet.Write([]byte{0xaa, 0xbb})
	writeVarint(7<<3 | 5) // volume_adjustment fixed32
	var volume [4]byte
	binary.LittleEndian.PutUint32(volume[:], math.Float32bits(0.75))
	packet.Write(volume[:])
	writeVarint(6<<3 | 2) // packed positional_data
	writeVarint(12)
	for _, f := range []uint32{math.Float32bits(1), math.Float32bits(2), math.Float32bits(3)} {
		var buf [4]byte
		binary.LittleEndian.PutUint32(buf[:], f)
		packet.Write(buf[:])
	}

	session, frame, opusData, terminator, context, position, volumeAdjustment := decodeUDPAudio(packet.Bytes())
	if session != 123 || frame != 1<<32 || !bytes.Equal(opusData, []byte{0xaa, 0xbb}) || terminator || context != 3 {
		t.Fatalf("decoded unexpected audio: session=%d frame=%d opus=%x terminator=%v context=%d", session, frame, opusData, terminator, context)
	}
	if volumeAdjustment != 0.75 {
		t.Fatalf("volume adjustment = %v", volumeAdjustment)
	}
	if position == nil || *position != [3]float32{1, 2, 3} {
		t.Fatalf("position = %v, want [1 2 3]", position)
	}
}

func mustDecodeHex(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}
