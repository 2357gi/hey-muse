package voice

import (
	"context"
	"github.com/HuskerMinion/techo5/echod/internal/config"
	"testing"
)

func TestEarlyResultRequiresIdenticalFinalAudio(t *testing.T) {
	p := &earlySTT{pcm: []byte{1, 2}, text: "廊下の電気を消して", done: make(chan struct{})}
	close(p.done)
	ctx := context.WithValue(context.Background(), earlySTTKey{}, p)
	calls := 0
	m := viaMuse{transcribe: func(context.Context, config.Brain, []byte) (string, error) {
		calls++
		return "廊下の電気を消してはいけない", nil
	}}
	b := config.Brain{STT: "127.0.0.1:50024"}
	same, _ := m.finalTranscript(ctx, b, []byte{1, 2})
	more, _ := m.finalTranscript(ctx, b, []byte{1, 2, 3})
	if calls != 1 || hallDirection(same) != "off" || hallDirection(more) != "" {
		t.Fatal("an early command survived changed final audio")
	}
}
