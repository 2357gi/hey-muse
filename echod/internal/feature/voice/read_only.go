package voice

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/HuskerMinion/techo5/echod/internal/lib/hook"
	"github.com/HuskerMinion/techo5/echod/internal/lib/mothership"
)

// ReadOnlyView asks the screen to show an allowlisted local view using cached data.
// Listeners do no network I/O. The answer never comes from the classifier.
type ReadOnlyView struct {
	Intent  string
	Answer  string
	Handled bool
}

var ReadOnlyViews hook.Hook[*ReadOnlyView]

func validReadOnlyIntent(intent string) bool {
	switch intent {
	case "weather_today", "weather_tomorrow", "weather_week", "calendar_today", "calendar_tomorrow", "calendar_week", "calendar_next7":
		return true
	}
	return false
}

func exactCalendarIntent(text string) string {
	text = strings.TrimSpace(text)
	text = strings.TrimRight(text, "。！？!?")
	for _, suffix := range []string{"を教えてください", "を見せてください", "を表示してください", "を教えて", "を見せて", "を表示して"} {
		text = strings.TrimSuffix(text, suffix)
	}
	switch text {
	case "予定", "カレンダー", "今日の予定":
		return "calendar_today"
	case "明日の予定":
		return "calendar_tomorrow"
	case "今週の予定":
		return "calendar_week"
	case "7日間の予定", "一週間の予定", "1週間の予定":
		return "calendar_next7"
	}
	return ""
}

func readOnlyAnswer(intent string) (string, bool) {
	if !validReadOnlyIntent(intent) {
		return "", false
	}
	query := &ReadOnlyView{Intent: intent}
	ReadOnlyViews.Emit(query)
	return query.Answer, query.Handled
}

type readOnlyConfig struct {
	URL, Key string
	Jev      bool
}

func classifyReadOnly(ctx context.Context, text string) (string, error) {
	raw, err := os.ReadFile("/data/misc/techo5/idle-home.json")
	if err != nil {
		return "", nil
	}
	var cfg readOnlyConfig
	if json.Unmarshal(raw, &cfg) != nil || !cfg.Jev {
		return "", nil
	}
	return requestReadOnly(ctx, cfg, text)
}

func requestReadOnly(ctx context.Context, cfg readOnlyConfig, text string) (string, error) {
	if !cfg.Jev || len(cfg.Key) != 64 || !strings.HasSuffix(cfg.URL, "/echo-home/v1") || utf8.RuneCountInString(text) > 320 {
		return "", nil
	}
	ctx, cancel := context.WithTimeout(ctx, 2500*time.Millisecond)
	defer cancel()
	endpoint := strings.TrimSuffix(cfg.URL, "/echo-home/v1") + "/echo-intent/v1"
	var result struct {
		OK     bool
		Intent string
	}
	err := mothership.HomeRequest(ctx, endpoint, cfg.Key, map[string]any{"op": "classify_read_only", "text": text, "time": time.Now().Unix()}, &result)
	if err != nil {
		return "", err
	}
	if !result.OK || !validReadOnlyIntent(result.Intent) {
		return "", nil
	}
	return result.Intent, nil
}
