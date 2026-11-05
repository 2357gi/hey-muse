package voice

import (
	"context"
	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/lib/muse"
	"testing"
)

func TestHallDirection(t *testing.T) {
	for text, want := range map[string]string{
		"廊下の電気を消して。": "off", "廊下のライトを点けてください": "on", "廊下の照明オフにして": "off",
		"廊下の電気を消して？": "", "廊下の電気を消していい？": "", "廊下の電気をつけないで": "",
		"電気を消して": "", "廊下と玄関の電気を消して": "", "「廊下の電気を消して」と言って": "",
		"廊下の電気を消してから点けて": "", "廊下の照明はついてる？": "",
	} {
		if got := hallDirection(text); got != want {
			t.Errorf("%q: %q, want %q", text, got, want)
		}
	}
}
func TestHallReceiptScoped(t *testing.T) {
	h := hallTurns{session: "one", receipt: "廊下の点灯を受け付けました。"}
	if h.withReceipt("two", "次") != "次" || h.withReceipt("one", "次") == "次" || h.withReceipt("one", "次") != "次" {
		t.Fatal("receipt scope")
	}
}

func TestUnsafeHomeUtterancesNeverReachMuse(t *testing.T) {
	for _, phrase := range []string{"廊下の電気を売りにして。", "それを消さないで。", "廊下の天気消して。", "廊下の電気はついてる？", "照明を消さないで", "電気をつけて", "廊下の電気を消してって言ったらどうなる？", "もう一度"} {
		link := &textTestLink{}
		m := newViaMuse(link, builtIn, nil, nil)
		m.hall.session, m.hall.receipt = "session", "廊下の点灯を受け付けました。"
		m.brain = func() config.Brain { return config.Brain{STT: "local"} }
		m.transcribe = func(context.Context, config.Brain, []byte) (string, error) { return phrase, nil }
		r, e := m.ask(context.Background(), someAudio, "session", func(muse.ReplyEvent) {})
		if e != nil || link.text != "" || r.Text != hallHoldReply(phrase) {
			t.Fatalf("unsafe route %q", phrase)
		}
	}
}
