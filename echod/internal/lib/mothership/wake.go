// Package mothership wakes the configured LAN bridge for interactive requests.
package mothership

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"time"
)

type Target struct {
	Address   string
	MAC       string
	Broadcast string
}

func Wait(ctx context.Context, endpoint string) error {
	raw, err := os.ReadFile("/data/misc/techo5/mothership.json")
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var target Target
	if err := json.Unmarshal(raw, &target); err != nil {
		return err
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return err
	}
	if u.Scheme != "http" || u.Host != target.Address {
		return nil
	}
	mac, err := net.ParseMAC(target.MAC)
	broadcast := net.ParseIP(target.Broadcast).To4()
	if err != nil || len(mac) != 6 || broadcast == nil {
		return errors.New("invalid mothership wake configuration")
	}
	ctx, cancel := context.WithTimeout(ctx, 100*time.Second)
	defer cancel()
	health := "http://" + target.Address + "/health"
	var nextWake time.Time
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, health, nil)
		if err != nil {
			return err
		}
		resp, probeErr := probe.Do(req)
		if probeErr == nil {
			io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
			if resp.StatusCode != http.StatusServiceUnavailable {
				return fmt.Errorf("mothership health: HTTP %d", resp.StatusCode)
			}
		} else if ctx.Err() == nil && time.Now().After(nextWake) {
			conn, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: broadcast, Port: 9})
			if err != nil {
				return err
			}
			_, err = conn.Write(magicPacket(mac))
			conn.Close()
			if err != nil {
				return err
			}
			nextWake = time.Now().Add(8 * time.Second)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("waiting for mothership: %w", ctx.Err())
		case <-time.After(time.Second):
		}
	}
}

var probe = &http.Client{Timeout: 1500 * time.Millisecond, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

func magicPacket(mac net.HardwareAddr) []byte {
	return append(bytes.Repeat([]byte{0xff}, 6), bytes.Repeat(mac, 16)...)
}
