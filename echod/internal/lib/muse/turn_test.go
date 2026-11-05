package muse

import (
	"encoding/json"
	"strconv"
	"testing"
)

func TestTaskCompletionWaitsForAllCurrentTasks(t *testing.T) {
	turn := newTurn()
	turn.acknowledged("our-message")
	status := func(id, state, parent string) {
		turn.add(chatEvent{Event: "task.status", Payload: map[string]json.RawMessage{
			"task_id": json.RawMessage(strconv.Quote(id)), "status": json.RawMessage(strconv.Quote(state)),
			"reply_to_message_id": json.RawMessage(strconv.Quote(parent)),
		}})
	}
	status("old", "completed", "")
	if turn.taskFinished {
		t.Fatal("replayed completion ended a new turn")
	}
	status("foreign", "running", "another-message")
	if turn.busy {
		t.Fatal("foreign task affected this turn")
	}
	status("one", "running", "")
	status("two", "running", "")
	status("one", "completed", "")
	if turn.taskFinished || !turn.busy {
		t.Fatal("another task is still running")
	}
	status("two", "completed", "")
	if !turn.taskFinished || turn.busy {
		t.Fatal("both tasks completed")
	}
	status("next", "running", "")
	if turn.taskFinished || !turn.busy {
		t.Fatal("new work must hold the turn open")
	}
}
