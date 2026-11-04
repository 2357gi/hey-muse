//go:build !dot

package display

import (
	"strings"

	"golang.org/x/image/font"
)

// wrapText keeps ordinary word boundaries, but also splits a run wider than the
// screen. Japanese sentences need this because they usually contain no spaces.
func wrapText(face font.Face, text string, maxW int) []string {
	if face == nil || maxW <= 0 {
		return nil
	}
	var lines []string
	line := ""
	width := func(s string) int { return font.MeasureString(face, s).Ceil() }
	for _, word := range strings.Fields(text) {
		if line != "" {
			if width(line+" "+word) <= maxW {
				line += " " + word
				continue
			}
			lines = append(lines, line)
			line = ""
		}
		if width(word) <= maxW {
			line = word
			continue
		}
		for _, r := range word {
			next := line + string(r)
			if line != "" && width(next) > maxW {
				lines = append(lines, line)
				line = string(r)
			} else {
				line = next
			}
		}
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines
}
