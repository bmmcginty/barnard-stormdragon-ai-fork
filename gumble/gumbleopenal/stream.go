package gumbleopenal

import (
    "encoding/binary"
    "errors"
    "os/exec"
    "time"

    "git.stormux.org/storm/barnard/gumble/gumble"
    "git.stormux.org/storm/barnard/gumble/go-openal/openal"
)

const (
    maxBufferSize = 11520  // Max frame size (2880) * bytes per stereo sample (4)
)

var (
    ErrState        = errors.New("gumbleopenal: invalid state")
    ErrMic          = errors.New("gumbleopenal: microphone disconnected or misconfigured")
    ErrInputDevice  = errors.New("gumbleopenal: invalid input device or parameters")
    ErrOutputDevice = errors.New("gumbleopenal: invalid output device or parameters")
)

func beep() {
    cmd := exec.Command("beep")
    cmdout, err := cmd.Output()
    if err != nil {
        panic(err)
    }
    if cmdout != nil {
    }
}

type Stream struct {
    client *gumble.Client
    link   gumble.Detacher

    deviceSource    *openal.CaptureDevice
    sourceFrameSize int
    micVolume      float32
    sourceStop     chan bool

    deviceSink  *openal.Device
    contextSink *openal.Context
}

func New(client *gumble.Client, inputDevice *string, outputDevice *string, test bool) (*Stream, error) {
    frmsz := 480
    if !test {
        frmsz = client.Config.AudioFrameSize()
    }

    // Always use mono for input device
    idev := openal.CaptureOpenDevice(*inputDevice, gumble.AudioSampleRate, openal.FormatMono16, uint32(frmsz))
    if idev == nil {
        return nil, ErrInputDevice
    }

    odev := openal.OpenDevice(*outputDevice)
    if odev == nil {
        idev.CaptureCloseDevice()
        return nil, ErrOutputDevice
    }

    if test {
        idev.CaptureCloseDevice()
        odev.CloseDevice()
        return nil, nil
    }

    s := &Stream{
        client:          client,
        sourceFrameSize: frmsz,
        micVolume:      1.0,
    }

    s.deviceSource = idev
    if s.deviceSource == nil {
        return nil, ErrInputDevice
    }

    s.deviceSink = odev
    if s.deviceSink == nil {
        return nil, ErrOutputDevice
    }
    s.contextSink = s.deviceSink.CreateContext()
    if s.contextSink == nil {
        s.Destroy()
        return nil, ErrOutputDevice
    }
    s.contextSink.Activate()

    return s, nil
}

func (s *Stream) AttachStream(client *gumble.Client) {
    s.link = client.Config.AttachAudio(s)
}

func (s *Stream) Destroy() {
    if s.link != nil {
        s.link.Detach()
    }
    if s.deviceSource != nil {
        s.StopSource()
        s.deviceSource.CaptureCloseDevice()
        s.deviceSource = nil
    }
    if s.deviceSink != nil {
        s.contextSink.Destroy()
        s.deviceSink.CloseDevice()
        s.contextSink = nil
        s.deviceSink = nil
    }
}

func (s *Stream) StartSource(inputDevice *string) error {
    if s.sourceStop != nil {
        return ErrState
    }
    if s.deviceSource == nil {
        return ErrMic
    }
    s.deviceSource.CaptureStart()
    s.sourceStop = make(chan bool)
    go s.sourceRoutine(inputDevice)
    return nil
}

func (s *Stream) StopSource() error {
    if s.deviceSource == nil {
        return ErrMic
    }
    s.deviceSource.CaptureStop()
    if s.sourceStop == nil {
        return ErrState
    }
    close(s.sourceStop)
    s.sourceStop = nil
    return nil
}

func (s *Stream) GetMicVolume() float32 {
    return s.micVolume
}

func (s *Stream) SetMicVolume(change float32, relative bool) {
    var val float32
    if relative {
        val = s.GetMicVolume() + change
    } else {
        val = change
    }
    if val >= 1 {
        val = 1.0
    }
    if val <= 0 {
        val = 0
    }
    s.micVolume = val
}

func (s *Stream) OnAudioStream(e *gumble.AudioStreamEvent) {
    go func(e *gumble.AudioStreamEvent) {
        var source = openal.NewSource()
        e.User.AudioSource = &source
        
        // Set initial gain based on volume and mute state
        if e.User.LocallyMuted {
            e.User.AudioSource.SetGain(0)
        } else {
            e.User.AudioSource.SetGain(e.User.Volume)
        }

        bufferCount := e.Client.Config.Buffers
        if bufferCount < 64 {
            bufferCount = 64
        }
        emptyBufs := openal.NewBuffers(bufferCount)
        
        reclaim := func() {
            if n := source.BuffersProcessed(); n > 0 {
                reclaimedBufs := make(openal.Buffers, n)
                source.UnqueueBuffers(reclaimedBufs)
                emptyBufs = append(emptyBufs, reclaimedBufs...)
            }
        }

        var raw [maxBufferSize]byte
        
        for packet := range e.C {
            // Skip processing if user is locally muted
            if e.User.LocallyMuted {
                continue
            }

            var boost uint16 = uint16(1)
            samples := len(packet.AudioBuffer)
            if samples > cap(raw)/2 {
                continue
            }
            
            boost = e.User.Boost

            // Check if sample count suggests stereo data
            isStereo := samples > gumble.AudioDefaultFrameSize && samples%2 == 0
            format := openal.FormatMono16
            if isStereo {
                format = openal.FormatStereo16
                samples = samples / 2
            }

            rawPtr := 0
            if isStereo {
                // Process stereo samples as pairs
                for i := 0; i < samples*2; i += 2 {
                    // Process left channel
                    sample := packet.AudioBuffer[i]
                    if boost > 1 {
                        sample = int16((int32(sample) * int32(boost)))
                    }
                    binary.LittleEndian.PutUint16(raw[rawPtr:], uint16(sample))
                    rawPtr += 2

                    // Process right channel
                    sample = packet.AudioBuffer[i+1]
                    if boost > 1 {
                        sample = int16((int32(sample) * int32(boost)))
                    }
                    binary.LittleEndian.PutUint16(raw[rawPtr:], uint16(sample))
                    rawPtr += 2
                }
            } else {
                // Process mono samples
                for i := 0; i < samples; i++ {
                    sample := packet.AudioBuffer[i]
                    if boost > 1 {
                        sample = int16((int32(sample) * int32(boost)))
                    }
                    binary.LittleEndian.PutUint16(raw[rawPtr:], uint16(sample))
                    rawPtr += 2
                }
            }

            reclaim()
            if len(emptyBufs) == 0 {
                continue
            }

            last := len(emptyBufs) - 1
            buffer := emptyBufs[last]
            emptyBufs = emptyBufs[:last]

            buffer.SetData(format, raw[:rawPtr], gumble.AudioSampleRate)
            source.QueueBuffer(buffer)

            if source.State() != openal.Playing {
                source.Play()
            }
        }
        reclaim()
        emptyBufs.Delete()
        source.Delete()
    }(e)
}

func (s *Stream) sourceRoutine(inputDevice *string) {
    interval := s.client.Config.AudioInterval
    frameSize := s.client.Config.AudioFrameSize()

    if frameSize != s.sourceFrameSize {
        s.deviceSource.CaptureCloseDevice()
        s.sourceFrameSize = frameSize
        // Always use mono for input
        s.deviceSource = openal.CaptureOpenDevice(*inputDevice, gumble.AudioSampleRate, openal.FormatMono16, uint32(s.sourceFrameSize))
    }

    ticker := time.NewTicker(interval)
    defer ticker.Stop()

    stop := s.sourceStop

    outgoing := s.client.AudioOutgoing()
    defer close(outgoing)

    for {
        select {
        case <-stop:
            return
        case <-ticker.C:
            buff := s.deviceSource.CaptureSamples(uint32(frameSize))
            if len(buff) != frameSize*2 {
                continue
            }
            int16Buffer := make([]int16, frameSize)
            for i := range int16Buffer {
                sample := int16(binary.LittleEndian.Uint16(buff[i*2:]))
                if s.micVolume != 1.0 {
                    sample = int16(float32(sample) * s.micVolume)
                }
                int16Buffer[i] = sample
            }
            outgoing <- gumble.AudioBuffer(int16Buffer)
        }
    }
}
