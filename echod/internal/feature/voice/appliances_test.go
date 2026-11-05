package voice

import "testing"

func TestApplianceCommands(t *testing.T) {
	for phrase, want := range map[string][2]string{
		"電気消して": {"living", "off"}, "電気を消してください。": {"living", "off"},
		"モニター消して": {"monitor", "toggle"}, "モニター付けて": {"monitor", "toggle"},
		"モニターをつけて。": {"monitor", "toggle"}, "モニターを点けて": {"monitor", "toggle"},

		"扇風機を回して": {"fan", "on"}, "エアコンを止めて": {"ac", "off"}, "リビングの照明を消灯して": {"living", "off"},
		"廊下のライトをオフにして。":    {"hall", "off"},
		"リビングの電気つけて":       {"living", "on"},
		"リビングの照明を消して。":     {"living", "off"},
		"エアコンの電源を入れてください":  {"ac", "on"},
		"サーキュレーターをつけて":     {"fan", "on"},
		"扇風機を消して":          {"fan", "off"},
		"プラグミニをオンにして":      {"plug", "on"},
		"掃除機の電源を切って":       {"vacuum", "off"},
		"カーテンを開けて":         {"curtain", "open"},
		"カーテンを閉めてください":     {"curtain", "close"},
		"モニターの電源を切り替えて":    {"monitor", "toggle"},
		"プロジェクターの電源を切り替えて": {"projector", "toggle"},
	} {
		got, ok := parseAppliance(phrase)
		if !ok || got.target != want[0] || got.action != want[1] {
			t.Errorf("%q => %+v, %t", phrase, got, ok)
		}
	}
	h := hallTurns{}
	for _, phrase := range []string{"ボーカルライト消した", "電気を消して？", "電気消さないで", "「電気消して」と言って", "カーテンを開けないで", "カーテンを開けて？", "モニターをつけていい？", "モニター消さないで", "モニター消してから付けて", "プロジェクターを消して", "ミラーボールをつけて", "エアコンと扇風機をつけて", "「掃除機をつけて」と言って", "リビングの電気を消してからつけて", "それを開けて"} {
		if _, ok := parseAppliance(phrase); ok || !h.hold("chat", phrase) {
			t.Errorf("unsafe routing for %q", phrase)
		}
	}
}
