//go:build !dot && !spot

package display

import (
	"fmt"
	"github.com/HuskerMinion/techo5/echod/internal/feature/home"
	"github.com/HuskerMinion/techo5/echod/internal/lib/hass"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestJapaneseWeatherView(t *testing.T) {
	now := time.Date(2026, 11, 5, 16, 0, 0, 0, time.Local)
	s := scene{now: now, phase: "idle", weather: home.Weather{Temp: "18°", Condition: "partlycloudy"}, weatherPlace: "テスト地域", forecastAt: now}
	for i := 0; i < 7; i++ {
		s.forecast = append(s.forecast, hass.Day{When: now.AddDate(0, 0, i), Condition: []string{"sunny", "rainy", "cloudy"}[i%3], High: 20, Low: 12, Rain: []int{0, 100, -1}[i%3]})
	}
	temp, humidity, co2, battery := 24.5, 52.0, 700.0, 90.0
	s.home = standbyView{Enabled: true, Environment: &homeEnvironment{Temperature: &temp, Humidity: &humidity, CO2: &co2, Battery: &battery}, EnvironmentUpdated: now, Updated: now, Scenes: []homeScene{{"a", "照明をつける", "mdi:lightbulb"}, {"b", "照明を消す", "mdi:lightbulb-off"}, {"c", "寝る直前", "mdi:bed"}, {"d", "暖房", "mdi:air-conditioner"}, {"e", "カーテン", "mdi:curtains"}, {"f", "掃除機", "mdi:robot-vacuum"}}}
	original := s
	for _, size := range []image.Point{{960, 480}, {1280, 800}} {
		s = original
		r := newRenderer(image.NewRGBA(image.Rectangle{Max: size}))
		for _, view := range []string{"standby", "forecast", "unavailable"} {
			s.showWeather = view != "standby"
			if view == "unavailable" {
				s.forecast = nil
				s.weather = home.Weather{}
			}
			r.draw(s)
			if view == "standby" {
				found := false
				for _, z := range r.homeZones {
					if z.action == "weather" {
						found = true
						if !z.bounds.In(r.dst.Bounds()) {
							t.Fatal("weather touch outside panel")
						}
					}
				}
				if !found {
					t.Fatal("missing weather button")
				}
			}
			if dir := os.Getenv("WEATHER_PREVIEW"); dir != "" {
				name := fmt.Sprintf("%s-%d.png", view, size.X)
				f, err := os.Create(filepath.Join(dir, name))
				if err != nil {
					t.Fatal(err)
				}
				if err = png.Encode(f, r.dst); err != nil {
					t.Fatal(err)
				}
				f.Close()
			}
		}
	}
	if rainChance(0) != "0%" || rainChance(-1) != "—" || rainChance(100) != "100%" {
		t.Fatal("rain probability")
	}
}

func TestStandbyWeatherTap(t *testing.T) {
	d := &Display{r: testRenderer(), poke: make(chan struct{}, 1)}
	d.r.drawStandby(scene{now: time.Now(), phase: "idle", home: standbyView{Enabled: true}})
	if !d.tapStandby(image.Pt(100, 150)) || !d.weatherUntil.After(time.Now()) || d.radar {
		t.Fatal("weather tap did not open forecast")
	}
	d.r.draw(scene{now: time.Now(), phase: "thinking"})
	if d.tapStandby(image.Pt(100, 150)) {
		t.Fatal("hidden weather button remained active")
	}
}
