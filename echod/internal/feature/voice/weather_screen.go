//go:build !dot

package voice

import "github.com/HuskerMinion/techo5/echod/internal/feature/home"

func localWeatherAnswer(text string) (string, bool) {
	return home.Get().WeatherAnswer(text)
}
