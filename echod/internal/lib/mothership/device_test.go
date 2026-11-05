//go:build mothership_device_test

package mothership

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"testing"
	"time"
)

type offlineOnce struct{ attempts int }

func (o *offlineOnce) RoundTrip(req *http.Request) (*http.Response, error) {
	o.attempts++
	if o.attempts == 1 {
		return nil, errors.New("simulate sleeping PC")
	}
	return http.DefaultTransport.RoundTrip(req)
}

// Run on the configured Echo. Exercise the real packet sender while the PC stays awake.
func TestDeviceWake(t *testing.T) {
	var target Target
	raw, err := os.ReadFile("/data/misc/techo5/mothership.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &target); err != nil {
		t.Fatal(err)
	}
	failFirst := &offlineOnce{}
	old := probe.Transport
	probe.Transport = failFirst
	defer func() { probe.Transport = old }()
	ctx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
	defer cancel()
	if err := Wait(ctx, "http://"+target.Address+"/v1"); err != nil {
		t.Fatal(err)
	}
	if failFirst.attempts < 2 {
		t.Fatal("missing bridge recovery probe")
	}
	t.Log("wake signal sent; real bridge health returned HTTP 200")
}
