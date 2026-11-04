//go:build !dot

package display

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"testing"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

func TestJapaneseTextFallback(t *testing.T) {
	for _, data := range [][]byte{goregular.TTF, gobold.TTF} {
		parsed, err := opentype.Parse(data)
		if err != nil {
			t.Fatal(err)
		}
		options := &opentype.FaceOptions{Size: 34, DPI: 72, Hinting: font.HintingFull}
		original, err := opentype.NewFace(parsed, options)
		if err != nil {
			t.Fatal(err)
		}
		defer original.Close()
		face, err := newTextFace(parsed, options)
		if err != nil {
			t.Fatal(err)
		}
		defer face.Close()
		if face.Metrics() != original.Metrics() {
			t.Fatal("Latin layout metrics changed")
		}
		for _, r := range "12:34 PM Hello, Muse!" {
			want, _ := original.GlyphAdvance(r)
			got, ok := face.GlyphAdvance(r)
			if !ok || got != want {
				t.Fatalf("Latin advance changed for %q", r)
			}
		}
		if face.Kern('A', 'V') != original.Kern('A', 'V') {
			t.Fatal("Latin kerning changed")
		}
		for _, r := range "こんにちは。こんばんは。日本語表示、音声認識！ターズ・パロメナ「設定」" {
			bounds, advance, ok := face.GlyphBounds(r)
			if !ok || advance <= 0 || bounds.Empty() {
				t.Fatalf("missing Japanese glyph %q", r)
			}
			rect, mask, _, _, ok := face.Glyph(fixed.P(0, 40), r)
			if !ok || mask == nil || rect.Empty() {
				t.Fatalf("cannot rasterize %q", r)
			}
		}
	}
}

// Optional visual artifact; normal test runs do not write files.
func TestJapaneseTextPreview(t *testing.T) {
	path := os.Getenv("TECHO5_FONT_PREVIEW")
	if path == "" {
		t.Skip("preview not requested")
	}
	parsed, _ := opentype.Parse(goregular.TTF)
	face, err := newTextFace(parsed, &opentype.FaceOptions{Size: 34, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		t.Fatal(err)
	}
	defer face.Close()
	img := image.NewRGBA(image.Rect(0, 0, 960, 480))
	d := font.Drawer{Dst: img, Src: image.NewUniform(color.White), Face: face}
	for i, line := range []string{"23:41  Muse", "こんばんは。TARSです。", "日本語の表示を確認しています。", "こんにちは。ご用件があればどうぞ。", "アレクサ / Hey Muse"} {
		d.Dot = fixed.P(40, 70+i*75)
		d.DrawString(line)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}
