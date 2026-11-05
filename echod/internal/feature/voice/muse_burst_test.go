package voice

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/hardware/speaker"
)

func TestMuseBurstKeepsAllAudioInOrder(t *testing.T) {
	// Two minutes of marked PCM arrive all at once from the speech endpoint.
	// Both the event queue and the playback queue have just one free slot.
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	s := newStream(0, 0, 0)
	s.chunks = make(chan playbackChunk, 1)
	c := &conversation{phase: phaseReplying, events: make(chan event, 1), reply: reply{stream: s}}
	input := make([]byte, 120*speaker.VoiceRate*2)
	for i := range input {
		input[i] = byte(i*31 + i/997)
	}
	go func() {
		for i := 0; i < len(input); i += 3200 {
			c.postMuse(event{kind: evStreamAudio, audio: input[i : i+3200], ctx: ctx})
		}
		c.postMuse(event{kind: evStreamEnd, ctx: ctx})
	}()
	var output bytes.Buffer
	for {
		select {
		case <-ctx.Done():
			t.Fatal("playback stalled")
		case e := <-c.events:
			c.handle(e)
		case chunk, ok := <-s.chunks:
			if !ok {
				if !bytes.Equal(output.Bytes(), input) {
					t.Fatalf("playback differs: got %d bytes, want %d", output.Len(), len(input))
				}
				return
			}
			output.Write(chunk.data)
			chunk.consumed <- true
		}
	}
}

func TestMuseCanCancelWhilePlaybackIsBusy(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	c := &conversation{events: make(chan event, 1)}
	done := make(chan struct{})
	go func() {
		c.postMuse(event{kind: evStreamAudio, ctx: ctx})
		close(done)
	}()
	queued := <-c.events
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancellation did not release synthesis")
	}
	// An already queued chunk may finish after the producer leaves.
	queued.consumed <- true
}

func TestShortAcknowledgmentFlushesBeforeStreamEnd(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	s := newStream(0, 0, 650*time.Millisecond)
	finished := make(chan error, 1)
	go func() { finished <- s.play(ctx, speaker.New(), func() {}) }()
	accepted := make(chan bool, 1)
	send := func(ch playbackChunk) {
		ch.consumed = accepted
		s.send(ch)
		select {
		case ok := <-accepted:
			if !ok {
				t.Fatal("dropped chunk")
			}
		case <-ctx.Done():
			t.Fatal("stalled")
		}
	}
	send(playbackChunk{data: make([]byte, 3200)})
	send(playbackChunk{partEnd: true})
	if s.playingAt.IsZero() || len(s.held) != 0 {
		t.Fatal("short acknowledgment is still waiting for the final reply")
	}
	send(playbackChunk{data: make([]byte, 6400)})
	s.done()
	if err := <-finished; err != nil || s.bytes != 9600 {
		t.Fatalf("final reply not delivered: bytes=%d err=%v", s.bytes, err)
	}
}
