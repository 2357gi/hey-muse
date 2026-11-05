//go:build !dot && !spot

package display

import (
	"image"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestHomeVoicePages(t *testing.T) {
	if !slices.Contains(Get().turnStyle.Options, "ホーム") {
		t.Fatal("home style missing from the registered selector")
	}
	r := testRenderer()
	s := scene{homeTurn: true, phase: "listening", now: time.Date(2026, 11, 4, 14, 20, 0, 0, time.Local), eq: &eqView{level: make([]float64, eqBands)}}
	for i := range s.eq.level {
		s.eq.level[i] = 0.6*math.Exp(-float64((i-8)*(i-8))/30) + 0.24*math.Exp(-float64((i-19)*(i-19))/11)
	}
	save := func(name string) {
		if dir := os.Getenv("ECHO_TURN_PREVIEW_DIR"); dir != "" {
			f, err := os.Create(filepath.Join(dir, name+".png"))
			if err != nil {
				t.Fatal(err)
			}
			err = png.Encode(f, r.dst)
			f.Close()
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	r.draw(s)
	save("echo-listening")
	s.phase = "thinking"
	s.heard = "今日の予定を教えて"
	r.draw(s)
	save("echo-transcript")
	s.phase = "replying"
	s.reply = "今日は14時から打ち合わせがあります。"
	r.draw(s)
	save("echo-reply")
	if r.tapVoicePage(image.Pt(852, 425)) {
		t.Fatal("single page has a hidden next button")
	}
	s.reply = strings.Repeat("読み上げを待たずに、日本語の長い返答を最後まで読めます。", 14)
	r.draw(s)
	if !r.tapVoicePage(image.Pt(852, 425)) {
		t.Fatal("long answer cannot advance")
	}
	r.draw(s)
	if r.turnPanel.page != 1 || !r.tapVoicePage(image.Pt(725, 425)) {
		t.Fatal("cannot return to the preceding page")
	}
	r.tapVoicePage(image.Pt(852, 425))
	s.phase = "listening"
	s.heard = ""
	s.reply = ""
	r.draw(s)
	if r.turnPanel.page != 0 || r.tapVoicePage(image.Pt(852, 425)) {
		t.Fatal("new turn kept the old navigation")
	}
}
