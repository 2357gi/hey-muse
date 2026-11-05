package voice

import (
	"bytes"
	"context"
	"log/slog"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/lib/endpoint"
)

type earlySTT struct {
	pcm  []byte
	done chan struct{}
	text string
	err  error
}
type earlySTTKey struct{}

// Audio runs on the ordered send queue. The background worker only recognizes;
// the regular end-of-speech decision remains the sole dispatch point.
func (m *viaMuse) Audio(frame []byte) error {
	if err := m.utterance.Audio(frame); err != nil {
		return err
	}
	if m.early != nil || m.nextProbe == 0 {
		return nil
	}
	m.mu.Lock()
	n := len(m.pcm)
	if m.ctx == nil || n < m.nextProbe || n > 8*32000 {
		m.mu.Unlock()
		return nil
	}
	m.nextProbe = n + 3200
	samples := samplesOf(m.pcm)
	trimmed := endpoint.Trim(samples)
	ctx := m.ctx
	m.mu.Unlock()
	// Trim preserves 300 ms of trailing room. A removed trailing window
	// indicates that an initial transcript may finish during the VAD hold.
	if len(trimmed) == 0 || &trimmed[len(trimmed)-1] == &samples[len(samples)-1] {
		return nil
	}
	b := m.brain()
	if b.STT != "127.0.0.1:50024" {
		m.nextProbe = 0
		return nil
	}
	p := &earlySTT{pcm: bytesOf(trimmed), done: make(chan struct{})}
	m.early = p
	go func() {
		defer close(p.done)
		ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		p.text, p.err = m.transcribe(ctx, b, p.pcm)
	}()
	return nil
}

func (m *viaMuse) finalTranscript(ctx context.Context, b config.Brain, pcm []byte) (string, error) {
	p, _ := ctx.Value(earlySTTKey{}).(*earlySTT)
	if p != nil && b.STT == "127.0.0.1:50024" && bytes.Equal(p.pcm, pcm) {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-p.done:
			if p.err == nil {
				slog.Info("local recognition reused complete matching audio")
				return p.text, nil
			}
		}
	}
	return m.transcribe(ctx, b, pcm)
}
