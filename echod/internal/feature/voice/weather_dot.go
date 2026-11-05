//go:build dot

package voice

// The local forecast opens a screen. A Dot keeps its existing Muse voice route;
// importing home here would cycle through the Dot's double-tap radio handler.
func localWeatherAnswer(string) (string, bool) { return "", false }
