package voice

import "github.com/HuskerMinion/techo5/echod/internal/lib/hook"

// State is what a turn looks like from outside: which phase it is in and the words so far. It is
// what a screen shows — "Listening…", then what was heard, then the answer — and it is a hook
// rather than a query because a screen wants to redraw the moment something changes, not poll.
//
// Phase is the conversation's own name for it: idle, listening, thinking, replying. Heard and Reply
// are kept through the idle that follows a turn, so whoever shows them can let them linger.
type State struct {
	Phase string
	Heard string
	Reply string
}

// DisplayPhase shows available words while the speaker prepares their audio.
func (s State) DisplayPhase() string {
	if s.Phase == "thinking" && s.Reply != "" {
		return "replying"
	}
	return s.Phase
}

// Changed fires on every phase change and whenever a transcript or a reply arrives. It runs on the
// conversation's goroutine, so listeners must not block.
var Changed hook.Hook[State]
