// Diagnostic for the exact socket client used by the microphone stream.
package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"github.com/HuskerMinion/techo5/echod/internal/lib/endpoint"
	"os"
	"time"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	if len(os.Args) != 2 {
		return fmt.Errorf("one 16 kHz PCM WAV required")
	}
	b, err := os.ReadFile(os.Args[1])
	if err != nil {
		return err
	}
	if len(b) < 44 || string(b[:4]) != "RIFF" || string(b[8:16]) != "WAVEfmt " || binary.LittleEndian.Uint16(b[20:]) != 1 || binary.LittleEndian.Uint16(b[22:]) != 1 || binary.LittleEndian.Uint32(b[24:]) != 16000 || binary.LittleEndian.Uint16(b[34:]) != 16 || string(b[36:40]) != "data" {
		return fmt.Errorf("expected canonical 16 kHz mono PCM WAV")
	}
	nbytes := int(binary.LittleEndian.Uint32(b[40:]))
	if nbytes > len(b)-44 || nbytes%2 != 0 {
		return fmt.Errorf("invalid data length")
	}
	b = b[44 : 44+nbytes]
	samples := make([]int16, len(b)/2)
	for i := range samples {
		samples[i] = int16(binary.LittleEndian.Uint16(b[2*i:]))
	}
	v, err := endpoint.DialNeural(context.Background(), "/run/echo-vad.sock")
	if err != nil {
		return err
	}
	defer v.Close()
	start := time.Now()
	end := false
	sent := 0
	for sent < len(samples) {
		count := min(320, len(samples)-sent)
		end, err = v.Feed(samples[sent : sent+count])
		if err != nil {
			return err
		}
		sent += count
		if end {
			break
		}
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"ended": end, "decision_seconds": float64(sent) / 16000, "speech_end_seconds": float64(v.EndedAt()) / 16000, "wall_seconds": time.Since(start).Seconds()})
}
