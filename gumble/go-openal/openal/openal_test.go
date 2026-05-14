package openal_test

import (
	"git.stormux.org/storm/barnard/gumble/go-openal/openal"
	"testing"
)

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
