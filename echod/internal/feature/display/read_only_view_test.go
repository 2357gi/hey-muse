//go:build !dot && !spot

package display

import (
	"strings"
	"testing"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/feature/voice"
)

func TestAgendaScopesIncludeOverlappingEvents(t *testing.T) {
	zone := time.FixedZone("test", 9*3600)
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, zone) // Sunday
	at := func(day, hour int) time.Time { return time.Date(2026, 11, day, hour, 0, 0, 0, zone) }
	events := []homeEvent{
		{Title: "past", Start: at(8, 9), End: at(8, 10)},
		{Title: "ongoing", Start: at(8, 11), End: at(8, 13)},
		{Title: "all day", Start: at(7, 0), End: at(10, 0), Day: true},
		{Title: "tomorrow", Start: at(9, 9), End: at(9, 10)},
		{Title: "outside", Start: at(15, 0), End: at(15, 1)},
	}
	for _, tc := range []struct {
		scope string
		count int
	}{{"calendar_today", 2}, {"calendar_tomorrow", 2}, {"calendar_week", 2}, {"calendar_next7", 3}} {
		got, _ := visibleAgenda(standbyView{Events: events, Scope: tc.scope, ScopeUntil: now.Add(time.Minute)}, now)
		if len(got) != tc.count {
			t.Errorf("%s: %d", tc.scope, len(got))
		}
	}
	got, title := visibleAgenda(standbyView{Events: events, Scope: "calendar_today", ScopeUntil: now.Add(-time.Second)}, now)
	if len(got) != 3 || title != "7日間の予定" {
		t.Fatal("expired filter did not return to default")
	}
}

func TestReadOnlyCalendarShowsRequestedScopeAndReportsUnavailableCache(t *testing.T) {
	d := &Display{poke: make(chan struct{}, 1)}
	d.standby.view = standbyView{Enabled: true, Updated: time.Now()}
	q := &voice.ReadOnlyView{Intent: "calendar_tomorrow"}
	d.readOnlyView(q)
	if !q.Handled || q.Answer != "明日の予定はありません。" || d.standby.snapshot().Scope != "calendar_tomorrow" || !d.quiet {
		t.Fatalf("view not selected: %+v", q)
	}
	d.standby.view.Unavailable = true
	q = &voice.ReadOnlyView{Intent: "calendar_today"}
	d.readOnlyView(q)
	if !q.Handled || strings.Contains(q.Answer, "予定はありません") {
		t.Fatal("unavailable cache reported no events")
	}
	d.standby.view.Unavailable = false
	d.standby.view.Events = make([]homeEvent, 24)
	q = &voice.ReadOnlyView{Intent: "calendar_tomorrow"}
	d.readOnlyView(q)
	if strings.Contains(q.Answer, "予定はありません") {
		t.Fatal("truncated cache reported no events")
	}
	d.standby.view.Enabled = false
	q = &voice.ReadOnlyView{Intent: "calendar_today"}
	d.readOnlyView(q)
	if q.Handled {
		t.Fatal("unconfigured dashboard swallowed request")
	}
}

func TestReadOnlyWeatherOpensForecastWithoutChangingOriginalRequest(t *testing.T) {
	for _, intent := range []string{"weather_today", "weather_tomorrow", "weather_week"} {
		d := &Display{poke: make(chan struct{}, 1), sheet: true}
		query := &voice.ReadOnlyView{Intent: intent}
		d.readOnlyView(query)
		if !query.Handled || query.Answer == "" || !d.weatherUntil.After(time.Now()) || d.sheet {
			t.Fatalf("forecast not selected: %+v", query)
		}
	}
	d := &Display{poke: make(chan struct{}, 1)}
	query := &voice.ReadOnlyView{Intent: "execute_scene"}
	d.readOnlyView(query)
	if query.Handled || !d.weatherUntil.IsZero() {
		t.Fatal("non-view operation accepted")
	}
}
