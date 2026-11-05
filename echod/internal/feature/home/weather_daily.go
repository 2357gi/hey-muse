package home

import (
	"fmt"
	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/lib/hass"
	"github.com/HuskerMinion/techo5/echod/internal/lib/locale"
	"strings"
	"time"
)

func forecastFrom(days []hass.Day, now time.Time) []hass.Day {
	today := now.Format("2006-01-02")
	var out []hass.Day
	for _, d := range days {
		if !d.When.IsZero() && d.When.In(now.Location()).Format("2006-01-02") >= today {
			out = append(out, d)
		}
	}
	return out
}

func (f *Feature) ForecastUpdated() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.fetched
}

// Only unambiguous requests for this device's local forecast are answered here.
func localWeatherQuestion(text string) (int, bool) {
	text = strings.NewReplacer(" ", "", "　", "", "？", "", "?", "", "。", "", "、", "", "／", "/").Replace(text)
	for _, suffix := range []string{"を教えてください", "を見せてください", "を表示してください", "を教えて", "を見せて", "を表示して", "は"} {
		text = strings.TrimSuffix(text, suffix)
	}
	switch text {
	case "天気", "今日の天気", "きょうの天気", "降水確率", "今日の降水確率", "今日の天気と降水確率", "今日の天気/降水確率":
		return 0, true
	case "明日の天気", "明日の降水確率":
		return 1, true
	case "今週の天気", "週間天気", "週間天気予報", "1週間の天気", "一週間の天気", "7日間の天気":
		return 7, true
	}
	return 0, false
}

func (f *Feature) WeatherAnswer(text string) (string, bool) {
	offset, ok := localWeatherQuestion(text)
	if !ok {
		return "", false
	}
	days := f.Forecast()
	if len(days) == 0 {
		f.Poke()
		return "天気を取得できていません。少し待ってから、もう一度お試しください。", true
	}
	name := config.Get().Home.Place.Name
	if name == "" {
		name = "設定地域"
	}
	if offset == 7 {
		return name + "の今日から7日間の天気と降水確率を表示します。", true
	}
	target := time.Now().AddDate(0, 0, offset).Format("2006-01-02")
	for _, d := range days {
		if d.When.Local().Format("2006-01-02") != target {
			continue
		}
		label := "今日"
		if offset == 1 {
			label = "明日"
		}
		rain := "降水確率は未取得です。"
		if d.Rain >= 0 {
			rain = fmt.Sprintf("降水確率は%dパーセントです。", d.Rain)
		}
		return fmt.Sprintf("%sの%sは%s、最高%.0f度、最低%.0f度。%s", name, label, locale.Sky(d.Condition, "ja"), d.High, d.Low, rain), true
	}
	return "その日の天気はまだ取得できていません。", true
}
