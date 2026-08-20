package openal_test

import (
	"git.stormux.org/storm/barnard/gumble/go-openal/openal"
	"testing"
)

// Regression: Buffer.Delete called the source deletion API, which reported an
// invalid source and leaked the buffer.
func TestBufferDeleteUsesBufferAPI(t *testing.T) {
	device := openal.OpenDevice("")
	if device == nil {
		t.Skip("OpenAL device is not available")
	}
	defer device.CloseDevice()
	context := device.CreateContext()
	if context == nil {
		t.Skip("OpenAL context is not available")
	}
	defer context.Destroy()
	context.Activate()
	buffer := openal.NewBuffer()
	if err := openal.Err(); err != nil {
		t.Fatal(err)
	}
	buffer.Delete()
	if err := openal.Err(); err != nil {
		t.Fatal(err)
	}
}

// Regression: public slice APIs indexed element zero before checking length,
// so harmless empty operations panicked before reaching OpenAL.
func TestEmptySliceAPIsDoNotPanic(t *testing.T) {
	var sources openal.Sources
	sources.Delete()
	sources.Play()
	sources.Stop()
	sources.Rewind()
	sources.Pause()
	var source openal.Source
	source.Setfv(0, nil)
	source.Setiv(0, nil)
	source.Getfv(0, nil)
	source.Getiv(0, nil)
	source.QueueBuffers(nil)
	source.UnqueueBuffers(nil)
	var device openal.Device
	if got := device.GetIntegerv(0, 0); len(got) != 0 {
		t.Fatalf("got %d integers", len(got))
	}
	var capture openal.CaptureDevice
	capture.CaptureTo(nil)
	capture.CaptureToInt16(nil)
	capture.CaptureStereo8To(nil)
	capture.CaptureStereo16To(nil)
	var listener openal.Listener
	listener.Setfv(0, nil)
	listener.Setiv(0, nil)
	listener.Getfv(0, nil)
	listener.Getiv(0, nil)
	var buffer openal.Buffer
	buffer.SetData(openal.FormatMono8, nil, 0)
	buffer.SetDataInt16(openal.FormatMono16, nil, 0)
	buffer.SetDataMono8(nil, 0)
	buffer.SetDataMono16(nil, 0)
	buffer.SetDataStereo8(nil, 0)
	buffer.SetDataStereo16(nil, 0)
	if got := openal.NewSources(0); len(got) != 0 {
		t.Fatalf("got %d sources", len(got))
	}
}

func TestGetVendor(t *testing.T) {
	device := openal.OpenDevice("")
	if device == nil {
		t.Skip("OpenAL device is not available")
	}
	defer device.CloseDevice()

	context := device.CreateContext()
	if context == nil {
		t.Skip("OpenAL context is not available")
	}
	defer context.Destroy()
	context.Activate()

	vendor := openal.GetVendor()

	if err := openal.Err(); err != nil {
		t.Fatal(err)
	} else if vendor == "" {
		t.Fatal("empty vendor returned")
	}
}
