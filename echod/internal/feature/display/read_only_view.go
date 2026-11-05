//go:build !dot && !spot

package display

import (
	"fmt"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/feature/home"
	"github.com/HuskerMinion/techo5/echod/internal/feature/voice"
)

func agendaWindow(scope string, now time.Time) (time.Time, time.Time, string) {
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	switch scope {
	case "calendar_today":
		return now, today.AddDate(0, 0, 1), "今日の予定"
	case "calendar_tomorrow":
		return today.AddDate(0, 0, 1), today.AddDate(0, 0, 2), "明日の予定"
	case "calendar_week":
		days := 7 - (int(now.Weekday())+6)%7
		return now, today.AddDate(0, 0, days), "今週の予定"
	default:
		return now, today.AddDate(0, 0, 7), "7日間の予定"
	}
}

func visibleAgenda(view standbyView, now time.Time) ([]homeEvent, string) {
	scope := view.Scope
	if !now.Before(view.ScopeUntil) {
		scope = ""
	}
	start, end, title := agendaWindow(scope, now)
	var events []homeEvent
	for _, ev := range view.Events {
		if ev.End.After(start) && ev.Start.Before(end) {
			events = append(events, ev)
		}
	}
	return events, title
}

func (d *Display) readOnlyView(query *voice.ReadOnlyView) {
	weatherText := map[string]string{"weather_today": "今日の天気", "weather_tomorrow": "明日の天気", "weather_week": "今週の天気"}[query.Intent]
	if weatherText != "" {
		query.Answer, query.Handled = home.Get().WeatherAnswer(weatherText)
		if query.Handled {
			d.ShowWeather(false)
		}
		return
	}
	switch query.Intent {
	case "calendar_today", "calendar_tomorrow", "calendar_week", "calendar_next7":
	default:
		return
	}
	now := time.Now()
	d.standby.mu.Lock()
	if !d.standby.view.Enabled {
		d.standby.mu.Unlock()
		return
	}
	d.standby.view.Scope, d.standby.view.ScopeUntil, d.standby.view.Page = query.Intent, now.Add(calendarShow), 0
	view := d.standby.view
	d.standby.mu.Unlock()
	events, title := visibleAgenda(view, now)
	query.Handled = true
	switch {
	case view.Unavailable || view.Updated.IsZero() || now.Sub(view.Updated) > 10*time.Minute:
		query.Answer = "予定を取得できていません。画面で取得状況を確認してください。"
	case len(view.Events) >= 24:
		query.Answer = "取得済みの予定を画面に表示します。"
	case len(events) == 0:
		query.Answer = title + "はありません。"
	default:
		query.Answer = fmt.Sprintf("%sは%d件です。画面に表示します。", title, len(events))
	}
	d.mu.Lock()
	d.quiet = true
	d.weatherArmed, d.weatherUntil = false, time.Time{}
	d.calUntil, d.calDetail = time.Time{}, nil
	d.sheet, d.drawer, d.dash, d.deckUp = false, false, false, false
	d.mu.Unlock()
	d.wake()
}

// A fixed title is all the renderer needs; classification never supplies UI text.
func emptyAgendaLabel(title string) string {
	return title + "はありません"
}
