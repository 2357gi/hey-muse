package voice

import (
	"slices"
	"testing"

	"github.com/HuskerMinion/techo5/echod/internal/hardware/mic"
)

func TestRequestStartsInLastChirpFrame(t *testing.T) {
	// 150 ms ends halfway through a normal 20 ms microphone frame.
	cue := mic.Rate * 150 / 1000
	samples := make([]int16, mic.FrameSamples*20)
	for i := range samples {
		samples[i] = int16(i)
	}
	capture := chirpCapture{skip: cue}
	var got []int16
	for i := 0; i < len(samples); i += mic.FrameSamples {
		got = append(got, capture.afterTone(samples[i:i+mic.FrameSamples])...)
	}
	if !slices.Equal(got, samples[cue:]) {
		t.Fatal("microphone frames did not preserve the complete request after the wake tone")
	}
}
