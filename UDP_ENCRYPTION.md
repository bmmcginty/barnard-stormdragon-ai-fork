# Mumble UDP Encryption & Protocol Versioning

Based on the official Mumble client source at `./mumble/` (v1.5.x / v1.6.x).

---

## 1. Protocol Versioning

The protocol version determines the UDP packet format.

| Version | UDP Format | Audio Type Byte | Ping Type Byte |
|---------|-----------|-----------------|-----------------|
| < 1.5.0 | Legacy (varint-based) | `(codec << 5) \| target` (e.g. `0x80` for Opus) | `(1 << 5) = 0x20` |
| >= 1.5.0 | Protobuf (MumbleUDP) | `0x00` | `0x01` |

The version boundary is defined in `MumbleProtocol.h`:

```cpp
constexpr Version::full_t PROTOBUF_INTRODUCTION_VERSION = Version::fromComponents(1, 5, 0);
```

The client advertises its version in the initial `Version` TCP message (field 1 = `VersionV1`).
The server sends its version in `CodecVersion` (TCP message type 21).

**Critical**: The UDP decoder checks the negotiated protocol version to decide which format
to use. However, it also auto-upgrades: if a protobuf-format ping (`0x01`) arrives while in
legacy mode, the version is bumped to >= 1.5.0.

```cpp
// From MumbleProtocol.cpp UDPDecoder::decode():
if (header == static_cast<byte>(UDPMessageType::Ping)) {
    // Upgrade to at least PROTOBUF_INTRODUCTION_VERSION
    this->setProtocolVersion(std::max(this->getProtocolVersion(), PROTOBUF_INTRODUCTION_VERSION));
    return decodePing_protobuf(...);
}
```

---

## 2. Encryption Layer: CryptStateOCB2

All UDP packets (both legacy and protobuf) share the same encryption layer.

### Wire Format

```
[iv_byte(1)] [tag(3)] [ciphertext(variable)]
```

Total overhead: **4 bytes** (ssize = plaintext_size + 4).

### Algorithm

- **AES-128-OCB** (not OCB2, despite the class name)
- Key: 16 bytes (from `CryptSetup` TCP message)
- Encrypt IV: 16 bytes (`client_nonce` from `CryptSetup`)
- Decrypt IV: 16 bytes (`server_nonce` from `CryptSetup`)

### IV Increment (Little-Endian)

The IV is a 16-byte integer incremented **little-endian** (byte 0 is the LSB):

```cpp
// From CryptStateOCB2::encrypt():
for (int i = 0; i < AES_BLOCK_SIZE; i++)
    if (++encrypt_iv[i])
        break;
```

Starts at byte 0, increments, breaks on non-overflow. This matches wumble's
`increment_encrypt_iv`. **Important**: byte 0 changes every packet.

### Encrypt

```cpp
// From CryptStateOCB2::encrypt():
// 1. Increment IV
for (int i = 0; i < AES_BLOCK_SIZE; i++)
    if (++encrypt_iv[i]) break;

// 2. OCB encrypt the plaintext
ocb_encrypt(source, dst+4, plain_length, encrypt_iv, tag);

// 3. Wire format: [iv_byte][tag[0..2]][ciphertext]
dst[0] = encrypt_iv[0];
dst[1] = tag[0];
dst[2] = tag[1];
dst[3] = tag[2];
```

### Decrypt (with IV Tracking)

```cpp
// From CryptStateOCB2::decrypt():
// 1. Read IV byte from wire
ivbyte = source[0];

// 2. Check if in-order: decrypt_iv[0] + 1 == ivbyte
if (((decrypt_iv[0] + 1) & 0xFF) == ivbyte) {
    if (ivbyte > decrypt_iv[0]) {
        decrypt_iv[0] = ivbyte;          // Normal forward
    } else if (ivbyte < decrypt_iv[0]) {
        decrypt_iv[0] = ivbyte;
        for (int i = 1; i < AES_BLOCK_SIZE; i++)
            if (++decrypt_iv[i]) break;  // Wrapped: carry to higher bytes
    }
} else {
    // Late/reorder handling with diff-based window (±30)
    // ... (see MumbleProtocol.cpp for full logic)
}

// 3. OCB decrypt
ocb_decrypt(source+4, dst, crypted_length-4, decrypt_iv, tag);

// 4. Verify tag (first 3 bytes)
if (memcmp(tag, source+1, 3) != 0) { /* auth failure */ }

// 5. Update replay history
decrypt_history[decrypt_iv[0]] = decrypt_iv[1];
```

### OCB Implementation Details

The OCB implementation (`CryptStateOCB2::ocb_encrypt` / `ocb_decrypt`):

- Uses OpenSSL's `EVP_aes_128_ecb` as the block cipher primitive
- Nonce is the full 16-byte IV (no bottom-bit clearing — different from legacy OCB2)
- No associated data
- Final partial block: pad block has `byte[15] = remaining * 8` (bit-length encoding)
- GF(2^128) doubling via `S2()` (multiply by 2) and `S3()` (multiply by 3)
- Reduction constant: `0x87`
- Includes XEX* attack mitigation: if the second-to-last plaintext block is all zeros
  except potentially the last byte, a bit is flipped to prevent the attack described in
  https://eprint.iacr.org/2019/311

---

## 3. Legacy UDP Audio Format (version < 1.5.0)

Used when negotiated protocol version < `PROTOBUF_INTRODUCTION_VERSION`.

### Encode (Server → Client)

```
[byte 0: header] [session varint] [seq varint] [Opus: size varint] [opus data] [optional: 3×float32 position]
```

- **Header byte**: `(codec_type << 5) | target`
  - `codec_type`: 0=CELT_Alpha, 1=Ping, 2=Speex, 3=CELT_Beta, 4=Opus
  - `target`: 5-bit target/context (0=normal, 1=shout, 2=whisper, 3=listen)
- **Session varint**: sender's session ID (present only in server→client direction)
- **Seq varint**: frame number (monotonic, 10ms units)
- **Opus size varint**: bit 13 (0x2000) is the terminator flag; bits 0-12 are the opus data length
- **Position**: 3× float32 (x, y, z), only if space remains after opus data

### Decode (Client)

From `UDPDecoder::decodeAudio_legacy()`:

```cpp
m_audioData.targetOrContext = data[0] & 0x1f;
m_audioData.usedCodec = codec;  // Opus = 4

// Read session (server→client only)
if (this->getRole() == Role::Client) {
    stream >> m_audioData.senderSession;
}

// Read frame number
stream >> m_audioData.frameNumber;

// Opus: size varint with terminator bit
stream >> helper;
payloadSize = helper & 0x1FFF;        // 13 bits for size
m_audioData.isLastFrame = helper & 0x2000;  // bit 13 = terminator

// Read opus data
m_audioData.payload = span(payloadBegin, payloadSize);

// Check for positional data
if (stream.left() == 3 * sizeof(float)) { ... }
```

---

## 4. Protobuf UDP Audio Format (version >= 1.5.0)

Uses `MumbleUDP::Audio` protobuf message. Defined in `MumbleUDP.proto`.

### Message Fields

| Field | Number | Type | Description |
|-------|--------|------|-------------|
| sender_session | 3 | uint32 | Session ID of the speaker (server→client only) |
| frame_number | 4 | uint64 | Frame number in 10ms units |
| opus_data | 5 | bytes | The encoded Opus frame |
| is_terminator | 16 | bool | End of audio transmission |
| positional_data | 7 | repeated float | X, Y, Z position (3 floats) |
| volume_adjustment | 8 | float | Volume adjustment factor (server→client) |
| context | 9 | uint32 | Audio context (server→client: normal/shout/whisper/listen) |
| target | 10 | uint32 | Voice target ID (client→server) |

### Encode (Client → Server)

From `UDPAudioEncoder::prepareAudioPacket_protobuf()`:

```cpp
m_audioMessage.set_frame_number(data.frameNumber);
m_audioMessage.set_opus_data(data.payload.data(), data.payload.size());
m_audioMessage.set_is_terminator(data.isLastFrame);

// Serialize protobuf with 1-byte header prefix
encodeProtobuf(m_audioMessage, m_byteBuffer, 1, MAX_UDP_PACKET_SIZE);
m_byteBuffer[0] = static_cast<byte>(UDPMessageType::Audio);  // 0x00
```

Then in `updateAudioPacket_protobuf()`:
```cpp
m_audioMessage.set_target(data.targetOrContext);
encodeProtobuf(m_audioMessage, m_byteBuffer, offset, MAX_UDP_PACKET_SIZE);
```

### Wire Format (inside crypto envelope)

```
[0x00] [protobuf: frame_number + opus_data + is_terminator] [protobuf: target]
```

The encoder splits into "static" (frame data) and "variable" (target/context, volume) parts
for efficient re-encoding when forwarding to multiple recipients.

---

## 5. Ping Format

### Legacy Ping

```
[header: 0x20] [timestamp varint]
```
Or extended (12/24 bytes): `[version uint32] [timestamp uint64] [user_count uint32] [max_users uint32] [max_bw uint32]`

### Protobuf Ping

```
[0x01] [protobuf: MumbleUDP::Ping]
```
Fields: `timestamp` (uint64), `request_extended_information` (bool), `server_version_v2` (uint32), `user_count` (uint32), `max_user_count` (uint32), `max_bandwidth_per_user` (uint32).

---

## 6. UDP Send Path (Client)

From `ServerHandler::sendMessage()`:

```cpp
void ServerHandler::sendMessage(const unsigned char *data, int len, bool force) {
    // data = encoded audio packet (legacy or protobuf)

    if (!force && (NetworkConfig::TcpModeEnabled() || !bUdp)) {
        // TCP tunnel: wrap in UDPTunnel message
        // [UDPTunnel type(2 bytes)] [length(4 bytes)] [data]
    } else {
        // Encrypt and send via UDP
        connection->csCrypt->encrypt(data, crypto.data(), len);
        qusUdp->writeDatagram(crypto.data(), len + 4, qhaRemote, usResolvedPort);
    }
}
```

The server chooses UDP vs TCP per-message based on whether UDP is established (`bUdp`).

---

## 7. UDP Receive Path (Client)

From `ServerHandler::udpReady()`:

```cpp
void ServerHandler::udpReady() {
    while (qusUdp->hasPendingDatagrams()) {
        // 1. Read from UDP socket
        qusUdp->readDatagram(encrypted, buflen, &senderAddr, &senderPort);

        // 2. Verify sender address/port matches server
        // 3. Check crypto is initialized
        // 4. Decrypt
        connection->csCrypt->decrypt(encrypted, buffer.data(), buflen);

        // 5. Decode based on protocol version
        m_udpDecoder.decode(buffer.subspan(0, buflen - 4));

        // 6. Dispatch
        switch (m_udpDecoder.getMessageType()) {
            case UDPMessageType::Ping:  /* measure latency */  break;
            case UDPMessageType::Audio: /* play audio */       break;
        }
    }
}
```

---

## 8. Barnard-Specific Findings

### Advertised Version

Barnard's `gumble` library sends `VersionV1 = 1<<16 | 3<<8 | 0` = **1.3.0**:

```go
// From gumble/gumble/client.go DialWithDialer():
versionPacket := MumbleProto.Version{
    VersionV1: proto.Uint32(ClientVersion),  // 1<<16 | 3<<8 | 0
    ...
}
```

This is **below 1.5.0**, so the server falls back to **legacy UDP format** for all audio
sent to barnard. This is why incoming packets have type byte `0x80` (legacy Opus) instead
of `0x00` (protobuf Audio).

### Fix

To receive protobuf-format audio, barnard should advertise version >= 1.5.0:

```go
const ClientVersion = 1<<16 | 5<<8 | 0  // 1.5.0
```

However, this change must be accompanied by full support for the protobuf UDP format
(both encode and decode), which is what we've implemented in `udp15.go`.

### Current State

- **Outbound**: We send protobuf-format audio (type `0x00`) encrypted with 1.5 OCB.
  The server accepts this because it recognizes the 1.5 crypto format regardless of
  the advertised version.

- **Inbound**: The server sends us legacy-format audio (type `0x80` in bits 5-7)
  encrypted with 1.5 OCB. Our `handleLegacyUDPVoice` correctly parses this format.

- **Both paths work** given the current hybrid setup.
