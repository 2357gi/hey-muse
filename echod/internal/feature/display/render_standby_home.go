//go:build !dot && !spot

package display

import (
	"fmt"
	"github.com/HuskerMinion/techo5/echod/internal/lib/locale"
	"image"
	"image/color"
	"image/draw"
)

func (r *renderer) drawStandby(s scene) {
	bg, panel, button := homeInk.bg, homeInk.panel, homeInk.button
	ink, muted, mint, gold := homeInk.text, homeInk.muted, homeInk.mint, homeInk.gold
	draw.Draw(r.dst, r.dst.Bounds(), image.NewUniform(bg), image.Point{}, draw.Src)
	rect := func(x, y, w, h int) image.Rectangle { return image.Rect(r.s(x), r.s(y), r.s(x+w), r.s(y+h)) }
	text := func(size int, bold bool, line string, x, y int, c color.RGBA) {
		r.text(r.styleFace(bold, size), line, r.s(x), r.s(y), c)
	}
	clock := s.now.Format("15:04")
	text(24, false, fmt.Sprintf("%d月%d日（%s）", s.now.Month(), s.now.Day(), []string{"日", "月", "火", "水", "木", "金", "土"}[s.now.Weekday()]), 30, 46, muted)
	text(72, true, clock, 24, 113, ink)
	weather := "天気を取得中"
	if s.weather.Condition != "" {
		weather = "屋外 " + s.weather.Temp + "  " + locale.Sky(s.weather.Condition, "ja")
	} else if !s.forecastAt.IsZero() {
		weather = "天気を更新できません"
	}
	f := r.styleFace(false, 20)
	r.text(f, r.fit(f, weather, r.s(284)), r.s(29), r.s(149), mint)
	rain := "今日の降水確率 —  ›"
	for _, day := range s.forecast {
		if day.When.Local().Format("2006-01-02") == s.now.Format("2006-01-02") && day.Rain >= 0 {
			rain = fmt.Sprintf("今日の降水確率 %d%%  ›", day.Rain)
			break
		}
	}
	text(20, false, rain, 29, 179, ink)

	env := s.home.Environment
	if env != nil {
		value := func(label string, n *float64, unit string, x, y int) {
			line := label + " —"
			if n != nil {
				line = fmt.Sprintf("%s %g%s", label, *n, unit)
			}
			text(20, false, line, x, y, ink)
		}
		value("室温", env.Temperature, "℃", 29, 209)
		value("湿度", env.Humidity, "%", 186, 209)
		value("CO₂", env.CO2, " ppm", 29, 238)
		value("電池", env.Battery, "%", 186, 238)
		prefix := "取得 "
		if s.home.EnvironmentUnavailable {
			prefix = "更新待ち・最終取得 "
		}
		text(16, false, prefix+s.home.EnvironmentUpdated.Local().Format("1/2 15:04"), 30, 262, muted)
	} else {
		message := "温湿度を取得中"
		if s.home.EnvironmentUnavailable {
			message = "温湿度を取得できません"
		}
		text(20, false, message, 30, 223, muted)
	}

	card := rect(330, 24, 604, 224)
	r.roundFill(card, 18, panel, panel)
	text(24, true, "予定", 353, 63, mint)
	var events []homeEvent
	for _, ev := range s.home.Events {
		if ev.End.After(s.now) {
			events = append(events, ev)
		}
	}
	pages := (len(events) + 2) / 3
	page := 0
	if pages > 0 {
		page = s.home.Page % pages
	}
	pageText := ""
	if pages > 1 {
		pageText = fmt.Sprintf("%d / %d  ›", page+1, pages)
	}
	if s.home.Unavailable {
		pageText = "更新待ち"
	}
	text(20, false, pageText, 818, 62, muted)
	if len(events) == 0 {
		line := "7日以内の予定はありません"
		if s.home.Updated.IsZero() {
			line = "予定を読み込み中"
		}
		if s.home.Unavailable {
			line = "カレンダーに接続できません"
		}
		text(24, false, line, 353, 155, ink)
	}
	for i := 0; i < 3 && 3*page+i < len(events); i++ {
		ev := events[3*page+i]
		y := 96 + 49*i
		date := ev.Start.Local().Format("1/2")
		if date == s.now.Format("1/2") {
			date = "今日"
		} else if date == s.now.AddDate(0, 0, 1).Format("1/2") {
			date = "明日"
		}
		when := ev.Start.Local().Format("15:04")
		if ev.Day {
			when = "終日"
		} else if ev.Start.Before(s.now) {
			when = "開催中"
		}
		lineColor := mint
		if ev.Calendar == "二人" {
			lineColor = gold
		}
		r.roundFill(rect(353, y-12, 4, 29), 2, lineColor, lineColor)
		text(20, false, date, 367, y+9, muted)
		text(20, true, when, 425, y+9, ink)
		f := r.styleFace(false, 24)
		r.text(f, r.fit(f, ev.Title, r.s(324)), r.s(506), r.s(y+10), ink)
		text(16, false, ev.Calendar, 847, y+9, lineColor)
	}
	text(25, true, "シーン", 28, 291, ink)
	if s.home.Status != "" {
		r.rightText(r.styleFace(false, 20), s.home.Status, r.s(931), r.s(291), mint)
	}
	var zones []homeZone
	if pages > 1 {
		zones = append(zones, homeZone{card, "agenda"})
	}
	for i, item := range s.home.Scenes {
		if i >= 6 {
			break
		}
		x, y := 26+(i%3)*307, 307+(i/3)*78
		b := rect(x, y, 294, 66)
		r.roundFill(b, 14, button, button)
		accent := []color.RGBA{mint, gold, {180, 178, 226, 255}, {167, 196, 229, 255}, {147, 209, 216, 255}}[i%5]
		r.mdiIcon(item.Icon, r.s(x+16), r.s(y+16), 32, accent)
		f := r.styleFace(false, 24)
		r.text(f, r.fit(f, item.Label, r.s(223)), r.s(x+59), r.s(y+42), ink)
		zones = append(zones, homeZone{b, item.ID})
	}
	zones = append(zones, homeZone{rect(24, 121, 294, 64), "weather"})
	r.zmu.Lock()
	r.homeZones = zones
	r.zmu.Unlock()
}
