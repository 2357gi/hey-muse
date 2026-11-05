package home

import (
	"github.com/HuskerMinion/techo5/echod/internal/lib/hass"
	"testing"
	"time"
)

func TestDailyForecastFreshness(t *testing.T) {
	now := time.Now()
	f := &Feature{fetched: now, forecast: []hass.Day{{When: now.AddDate(0, 0, -1)}, {When: now}, {When: now.AddDate(0, 0, 1)}}}
	if got := f.Forecast(); len(got) != 2 {
		t.Fatalf("expired day leaked: %v", got)
	}
	f.fetched = now.Add(-4 * time.Hour)
	if len(f.Forecast()) != 0 {
		t.Fatal("stale forecast shown")
	}
}
func TestLocalWeatherQuestions(t *testing.T) {
	for _, s := range []string{"今日の天気", "今日の降水確率を教えて", "今週の天気を見せて", "明日の天気は？"} {
		if _, ok := localWeatherQuestion(s); !ok {
			t.Error(s)
		}
	}
	for _, s := range []string{"大阪の今日の天気", "今週の天気についてメールを書いて", "雨音を再生して", "照明をつけて"} {
		if _, ok := localWeatherQuestion(s); ok {
			t.Error(s)
		}
	}
}
