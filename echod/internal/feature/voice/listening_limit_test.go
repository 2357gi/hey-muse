package voice

import (
	"github.com/HuskerMinion/techo5/echod/internal/config"
	"testing"
)

func TestLocalMuseListeningLimitFinishesAudioWithoutCanceling(t *testing.T) {
	c := thinkingTurn(t)
	c.be = &viaMuse{brain: func() config.Brain { return config.Brain{STT: "127.0.0.1:50024"} }}
	c.enter(phaseListening)
	stopped := false
	c.stopAudio = func() { stopped = true }
	c.handle(event{kind: evTimeout})
	if c.phase != phaseThinking || c.deadline == nil || !stopped {
		t.Fatalf("not thinking after ending capture: phase=%s stopped=%v", c.phase, stopped)
	}
	if len(c.out) != 1 {
		t.Fatalf("queued %d operations, want End only (no Stop)", len(c.out))
	}
}
func TestUnansweredFollowUpStillEndsQuietly(t *testing.T) {
	c := thinkingTurn(t)
	c.be = &viaMuse{brain: func() config.Brain { return config.Brain{STT: "127.0.0.1:50024"} }}
	c.enter(phaseListening)
	c.followUp = true
	c.handle(event{kind: evTimeout})
	if c.phase != phaseIdle {
		t.Fatalf("silent follow-up left in %s", c.phase)
	}
}
func TestMuseWithoutSeparateRecognizerKeepsListeningTimeout(t *testing.T) {
	c := thinkingTurn(t)
	c.be = &viaMuse{brain: func() config.Brain { return config.Brain{} }}
	c.enter(phaseListening)
	c.handle(event{kind: evTimeout})
	if c.phase != phaseIdle {
		t.Fatalf("original timeout changed to %s", c.phase)
	}
}
