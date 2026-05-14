package recording

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNormalizeFormat(t *testing.T) {
	tests := map[string]string{
		"":       "flac",
		" FLAC ": "flac",
		".opus":  "opus",
	}
	for input, want := range tests {
		if got := NormalizeFormat(input); got != want {
			t.Fatalf("NormalizeFormat(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestUniquePathAvoidsCollision(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 5, 14, 12, 30, 0, 0, time.Local)
	first := filepath.Join(dir, "barnard-recording-20260514-123000.flac")
	if err := os.WriteFile(first, []byte{}, 0600); err != nil {
		t.Fatal(err)
	}

	got := UniquePath(dir, now, "flac")
	want := filepath.Join(dir, "barnard-recording-20260514-123000-2.flac")
	if got != want {
		t.Fatalf("UniquePath() = %q, want %q", got, want)
	}
}

func TestNormalizeStereoFrame(t *testing.T) {
	mono := NormalizeStereoFrame([]int16{1, -2}, 3)
	wantMono := []int16{1, 1, -2, -2, 0, 0}
	for i := range wantMono {
		if mono[i] != wantMono[i] {
			t.Fatalf("mono[%d] = %d, want %d", i, mono[i], wantMono[i])
		}
	}

	stereo := NormalizeStereoFrame([]int16{1, 2, 3, 4, 5, 6}, 2)
	wantStereo := []int16{1, 2, 3, 4}
	for i := range wantStereo {
		if stereo[i] != wantStereo[i] {
			t.Fatalf("stereo[%d] = %d, want %d", i, stereo[i], wantStereo[i])
		}
	}
}

func TestMixSaturates(t *testing.T) {
	dst := []int16{32000, -32000, 10}
	mix(dst, []int16{2000, -2000, -20})
	want := []int16{32767, -32768, -10}
	for i := range want {
		if dst[i] != want[i] {
			t.Fatalf("dst[%d] = %d, want %d", i, dst[i], want[i])
		}
	}
}
