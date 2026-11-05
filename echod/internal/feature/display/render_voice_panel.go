//go:build !dot && !spot

package display

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
)

var homeTurnStyles = []turnStyle{{"ホーム", "home"}}

type voicePanel struct {
	// Navigation belongs to the touch and drawing goroutines, under zmu.
	prev, next image.Rectangle
	page       int
	// Wrapped text belongs to the renderer; a spectrum frame need not lay out the text again.
	text        string
	width, size int
	lines       []string
}

var homeInk = struct{ bg, panel, button, text, muted, mint, gold color.RGBA }{
	color.RGBA{17, 29, 33, 255}, color.RGBA{28, 43, 48, 255}, color.RGBA{35, 53, 58, 255},
	color.RGBA{242, 242, 227, 255}, color.RGBA{172, 196, 192, 255},
	color.RGBA{139, 217, 192, 255}, color.RGBA{237, 198, 135, 255},
}

func (r *renderer) tapVoicePage(p image.Point) bool {
	r.zmu.Lock()
	defer r.zmu.Unlock()
	switch {
	case p.In(r.turnPanel.next):
		r.turnPanel.page++
	case p.In(r.turnPanel.prev):
		r.turnPanel.page--
	default:
		return false
	}
	return true
}

func (r *renderer) drawVoicePanel(s scene) {
	p := homeInk
	if s.eq.night {
		p.bg, p.panel, p.button = color.RGBA{16, 9, 10, 255}, color.RGBA{33, 17, 17, 255}, color.RGBA{50, 26, 25, 255}
		p.text, p.muted, p.mint, p.gold = color.RGBA{222, 158, 139, 255}, color.RGBA{169, 114, 103, 255}, color.RGBA{219, 120, 94, 255}, color.RGBA{215, 147, 103, 255}
	}
	rect := func(x, y, w, h int) image.Rectangle { return image.Rect(r.s(x), r.s(y), r.s(x+w), r.s(y+h)) }
	text := func(size int, bold bool, v string, x, y int, c color.RGBA) {
		r.text(r.styleFace(bold, size), v, r.s(x), r.s(y), c)
	}
	draw.Draw(r.dst, r.dst.Bounds(), image.NewUniform(p.bg), image.Point{}, draw.Src)
	title, icon, accent := "聞き取り中", "mdi:microphone", p.mint
	switch s.phase {
	case "thinking":
		title, icon, accent = "文字起こし中", "mdi:message-text-outline", p.gold
		if s.heard != "" {
			title = "返答待ち"
		}
	case "replying", "lingering":
		title, icon = "Museの返答", "mdi:message-text-outline"
		if s.voiceAudio {
			title, icon = "読み上げ中", "mdi:volume-high"
		}
		if s.reply == "" {
			title = "聞き取り結果"
		}
	}
	r.roundFill(rect(28, 24, 56, 56), float64(r.s(16)), p.button, p.button)
	r.mdiIcon(icon, r.s(40), r.s(36), 32, accent)
	text(36, true, title, 104, 65, p.text)
	r.rightText(r.styleFace(false, 28), s.now.Format("15:04"), r.s(930), r.s(61), p.muted)
	card := rect(28, 104, 904, 348)
	r.roundFill(card, float64(r.s(22)), p.panel, p.panel)
	if s.phase == "listening" {
		text(24, false, "マイク入力", 54, 153, p.muted)
		// Each bar follows one measured frequency band, symmetrical about the baseline.
		for i, level := range s.eq.level {
			n := len(s.eq.level)
			x := 65 + i*835/n
			height := 6 + int(145*math.Sqrt(min(1, max(0, level))))
			col := mix(p.button, accent, 0.28+0.72*min(1, max(0, level)))
			r.roundFill(rect(x, 280-height/2, 14, height), float64(r.s(7)), col, col)
		}
		return
	}
	value := s.reply
	label := "返答"
	size, rows, step, base := 32, 5, 45, 197
	if value == "" {
		value, label = s.heard, "聞き取った内容"
		size, rows, step, base = 40, 4, 53, 220
	}
	if s.reply != "" && s.heard != "" {
		f := r.styleFace(false, 24)
		r.mdiIcon("mdi:microphone", r.s(54), r.s(124), 24, p.muted)
		r.text(f, r.fit(f, s.heard, r.s(810)), r.s(92), r.s(147), p.muted)
		draw.Draw(r.dst, rect(54, 166, 850, 1), image.NewUniform(p.button), image.Point{}, draw.Src)
		base = 215
	} else {
		text(24, false, label, 54, 153, p.muted)
	}
	if value == "" {
		for i := 0; i < 3; i++ {
			col := p.button
			if int(s.now.UnixMilli()/350%3) == i {
				col = accent
			}
			r.roundFill(rect(447+i*27, 265, 13, 13), float64(r.s(7)), col, col)
		}
		return
	}
	v := &r.turnPanel
	width := r.s(848)
	f := r.styleFace(false, size)
	if v.text != value || v.size != size || v.width != width {
		v.lines = r.wrap(f, value, width)
		v.text, v.size, v.width = value, size, width
	}
	pages := max(1, (len(v.lines)+rows-1)/rows)
	r.zmu.Lock()
	page := min(max(0, v.page), pages-1)
	v.page = page
	prev, next := rect(682, 402, 98, 44), rect(806, 402, 98, 44)
	interactive := !s.showVolume && !s.showDrawer && !s.announceRecording && !s.showAnnouncement && !s.showReminder && s.popup == nil
	if interactive && page > 0 {
		v.prev = prev
	}
	if interactive && page+1 < pages {
		v.next = next
	}
	r.zmu.Unlock()
	for j, line := range v.lines[page*rows : min((page+1)*rows, len(v.lines))] {
		r.text(f, line, r.s(54), r.s(base+j*step), p.text)
	}
	if pages > 1 {
		text(20, false, fmt.Sprintf("%d / %d", page+1, pages), 54, 432, p.muted)
		if page > 0 {
			r.roundFill(prev, float64(r.s(12)), p.button, p.button)
			text(20, true, "前へ", 710, 431, accent)
		}
		if page+1 < pages {
			r.roundFill(next, float64(r.s(12)), p.button, p.button)
			text(20, true, "次へ", 834, 431, accent)
		}
	}
}
