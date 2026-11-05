// Command configure-weather sets the device's forecast location explicitly.
package main

import (
	"flag"
	"fmt"
	"math"
	"os"
	"strings"

	"github.com/HuskerMinion/techo5/echod/internal/config"
)

func main() {
	path := flag.String("state", "/data/misc/techo5/state.json", "existing device state file")
	name := flag.String("name", "", "location display name")
	lat := flag.Float64("lat", math.NaN(), "latitude")
	lon := flag.Float64("lon", math.NaN(), "longitude")
	country := flag.String("country", "", "optional country code")
	flag.Parse()
	if flag.NArg() != 0 || strings.TrimSpace(*name) == "" || math.IsNaN(*lat) || math.IsNaN(*lon) || *lat < -90 || *lat > 90 || *lon < -180 || *lon > 180 {
		fmt.Fprintln(os.Stderr, "provide -name, -lat (-90..90), and -lon (-180..180)")
		os.Exit(2)
	}
	if _, err := os.Stat(*path); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	store, err := config.Load(*path)
	if err == nil {
		err = store.Update(func(c *config.Config) {
			c.Home.Place = config.Place{Name: *name, Lat: *lat, Lon: *lon, Country: strings.ToUpper(*country)}
			c.Home.Units = config.UnitsC
			c.Home.Weather = ""
		})
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("Weather location saved")
}
