package voice

import (
	"context"
	"errors"
	"testing"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/lib/muse"
)

type textTestLink struct {
	fakeLink
	text string
}

func (l *textTestLink) AskText(_ context.Context, text string, on func(muse.ReplyEvent)) (muse.Reply, error) {
	l.text = text
	on(muse.Heard{Text: "server echo"})
	on(muse.Settled{Text: "こんばんは"})
	return muse.Reply{Text: "こんばんは"}, nil
}

func TestMuseLocalRecognitionSendsTextAndKeepsTranscript(t *testing.T) {
	l := &textTestLink{fakeLink: fakeLink{ask: func(context.Context, []byte, func(muse.ReplyEvent)) (muse.Reply, error) {
		t.Fatal("audio was sent to Muse instead of the local transcript")
		return muse.Reply{}, nil
	}}}
	m := newViaMuse(l, builtIn, speaking(320, 320), func(event) {})
	m.brain = func() config.Brain { return config.Brain{STT: "local:50024", Language: "ja"} }
	m.transcribe = func(ctx context.Context, b config.Brain, pcm []byte) (string, error) {
		if b.Language != "ja" || len(pcm) == 0 {
			t.Fatal("missing language or audio")
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("unbounded recognition request")
		}
		return " 日本語で短く挨拶して ", nil
	}
	var heard []string
	reply, err := m.ask(context.Background(), someAudio, func(e muse.ReplyEvent) {
		if e, ok := e.(muse.Heard); ok {
			heard = append(heard, e.Text)
		}
	})
	if err != nil || l.text != "日本語で短く挨拶して" || reply.Heard != l.text || len(heard) != 1 || heard[0] != l.text {
		t.Fatalf("wrong text path: text=%q heard=%q reply=%+v err=%v", l.text, heard, reply, err)
	}
}

func TestMuseLocalRecognitionFailureKeepsVoiceFallback(t *testing.T) {
	called := 0
	l := &textTestLink{fakeLink: fakeLink{ask: func(_ context.Context, wav []byte, _ func(muse.ReplyEvent)) (muse.Reply, error) {
		called++
		if string(wav[:4]) != "RIFF" {
			t.Fatal("fallback is not WAV")
		}
		return muse.Reply{Text: "fallback"}, nil
	}}}
	m := newViaMuse(l, builtIn, speaking(320, 320), func(event) {})
	m.brain = func() config.Brain { return config.Brain{STT: "local:50024", Language: "ja"} }
	m.transcribe = func(context.Context, config.Brain, []byte) (string, error) { return "", errors.New("offline") }
	reply, err := m.ask(context.Background(), someAudio, func(muse.ReplyEvent) {})
	if err != nil || called != 1 || reply.Text != "fallback" || l.text != "" {
		t.Fatalf("fallback: %+v %v calls=%d", reply, err, called)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := m.ask(ctx, someAudio, func(muse.ReplyEvent) {}); !errors.Is(err, context.Canceled) || called != 1 {
		t.Fatal("canceled turn was sent to Muse")
	}
}

func TestMuseLocalSilenceDoesNotAsk(t *testing.T) {
	l := &textTestLink{fakeLink: fakeLink{ask: func(context.Context, []byte, func(muse.ReplyEvent)) (muse.Reply, error) {
		t.Fatal("silence should not be sent to Muse")
		return muse.Reply{}, nil
	}}}
	m := newViaMuse(l, builtIn, speaking(320, 320), func(event) {})
	m.brain = func() config.Brain { return config.Brain{STT: "local:50024"} }
	m.transcribe = func(context.Context, config.Brain, []byte) (string, error) { return " ", nil }
	if reply, err := m.ask(context.Background(), someAudio, func(muse.ReplyEvent) {}); err != nil || reply.Text != "" || l.text != "" {
		t.Fatal("silence was not ignored")
	}
}
