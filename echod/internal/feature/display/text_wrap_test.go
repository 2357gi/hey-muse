//go:build !dot

package display

import (
	"reflect"
	"strings"
	"testing"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
)

func TestWrapJapaneseWithoutSpaces(t *testing.T) {
	parsed, _ := opentype.Parse(goregular.TTF)
	face, err := newTextFace(parsed, &opentype.FaceOptions{Size: 34, DPI: 72})
	if err != nil {
		t.Fatal(err)
	}
	defer face.Close()
	text := strings.Repeat("日本語の長い文章も画面内に折り返して表示します。", 3)
	lines := wrapText(face, text, 880)
	if len(lines) < 2 || strings.Join(lines, "") != text {
		t.Fatalf("lost text: %q", lines)
	}
	for _, line := range lines {
		if font.MeasureString(face, line).Ceil() > 880 {
			t.Fatalf("line too wide: %q", line)
		}
	}
	width := font.MeasureString(face, "hello world").Ceil()
	if got := wrapText(face, "hello world again", width); !reflect.DeepEqual(got, []string{"hello world", "again"}) {
		t.Fatalf("ordinary word wrapping changed: %q", got)
	}
}
