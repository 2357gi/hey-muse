package triggers

import "testing"

func TestJapaneseWeather(t *testing.T) {
	for _, s := range []string{"今日の天気", "今日の降水確率", "今週の天気", "雨雲レーダー"} {
		if !AboutWeather(s, MatchAll) {
			t.Error(s)
		}
	}
	for _, s := range []string{"雨音を再生して", "雨の音を聞きたい", "照明をつけて"} {
		if AboutWeather(s, MatchAll) {
			t.Error(s)
		}
	}
}
