//go:build hall_device_test

package voice

import (
	"context"
	"fmt"
	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/lib/endpoint"
	"github.com/HuskerMinion/techo5/echod/internal/lib/muse"
	"golang.org/x/sys/unix"
	"os"
	"strings"
	"testing"
	"time"
)

// Pace the known fixture at microphone speed through the installed VAD and ASR.
// No dispatch is possible in this comparison.
func TestHallDeviceRecognition(t *testing.T) {
	for _, direction := range []string{"on", "off"} {
		for _, early := range []bool{false, true} {
			runHallFixture(t, direction, early, false)
		}
	}
}

// Opt-in optical measurement. The caller verifies the initial power and restores it.
func TestHallOpticalFixture(t *testing.T) {
	first := os.Getenv("HALL_TEST_DIRECTION")
	if first != "off" && first != "on" {
		t.Skip("requires an explicit direction")
	}
	runHallFixture(t, first, true, true)
}

func runHallFixture(t *testing.T, direction string, early, dispatch bool) {
	data, err := os.ReadFile("/data/echo-asr/hall-" + direction + ".wav")
	if err != nil {
		t.Fatal(err)
	}
	b := config.Brain{Language: "ja", STT: "127.0.0.1:50024"}
	m := &viaMuse{brain: func() config.Brain { return b }, transcribe: transcribe}
	if dispatch {
		m.link = &textTestLink{}
		m.transcribe = func(ctx context.Context, b config.Brain, pcm []byte) (string, error) {
			heard, err := transcribe(ctx, b, pcm)
			if err != nil || hallDirection(heard) != direction {
				return "", nil
			}
			return heard, nil
		}
	}
	if err := m.utterance.Start(""); err != nil {
		t.Fatal(err)
	}
	if early {
		m.nextProbe = 32000
	}
	v, err := endpoint.DialNeural(context.Background(), "/run/echo-vad.sock")
	if err != nil {
		t.Fatal(err)
	}
	var boot unix.Timespec
	if err := unix.ClockGettime(unix.CLOCK_BOOTTIME, &boot); err != nil {
		t.Fatal(err)
	}
	firstSample := float64(boot.Nano()) / 1e9
	start := time.Now()
	ended := false
	for i := 44; i+640 <= len(data); i += 640 {
		time.Sleep(time.Until(start.Add(time.Duration((i-44+640)/32) * time.Millisecond)))
		frame := data[i : i+640]
		if err = m.Audio(frame); err != nil {
			t.Fatal(err)
		}
		ended, err = v.Feed(samplesOf(frame))
		if err != nil {
			t.Fatal(err)
		}
		if ended {
			break
		}
	}
	_ = v.Close()
	if !ended {
		t.Fatal("no VAD endpoint")
	}
	ctx, pcm := m.take()
	if m.early != nil {
		ctx = context.WithValue(ctx, earlySTTKey{}, m.early)
	}
	rec := time.Now()
	if dispatch {
		speechEnd := 2.0
		if direction == "on" {
			speechEnd = 2.03
		}
		fmt.Printf("fixture_speech_end=%.4f direction=%s\n", firstSample+speechEnd, direction)
		reply, err := m.ask(ctx, pcm, "fixture", func(muse.ReplyEvent) {})
		if err != nil || hallDirection(reply.Heard) != direction || !strings.HasSuffix(reply.Text, "を受け付けました。") {
			t.Fatalf("fixture did not reach the requested direction")
		}
		fmt.Printf("normal_voice_route accepted direction=%s\n", direction)
	} else {
		text, err := m.finalTranscript(ctx, b, bytesOf(endpoint.Trim(samplesOf(pcm))))
		if err != nil {
			t.Fatal(err)
		}
		fmt.Printf("hall_fixture direction=%s early=%t vad_ms=%d recognition_after_vad_ms=%d matched=%t transcript=%q\n", direction, early, rec.Sub(start).Milliseconds(), time.Since(rec).Milliseconds(), hallDirection(text) == direction, text)
	}
	_ = m.Stop()
}
