//go:build !dot && !spot

package display

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	hexutil "encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestStandbySendsOnlyTheTappedScene(t *testing.T) {
	key := bytes.Repeat([]byte{71}, 32)
	block, _ := aes.NewCipher(key)
	gcm, _ := cipher.NewGCM(block)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mac := hmac.New(sha256.New, key)
		mac.Write(raw)
		if hexutil.EncodeToString(mac.Sum(nil)) != r.Header.Get("X-Echo-Signature") {
			t.Error("invalid device signature")
			return
		}
		b, _ := base64.StdEncoding.DecodeString(string(raw))
		p, err := gcm.Open(nil, b[:12], b[12:], []byte("echo-home-request-1"))
		var req struct{ Op, ID string }
		if err != nil || json.Unmarshal(p, &req) != nil || req.Op != "scene" || req.ID != "night" {
			t.Error("wrong scene")
			return
		}
		calls.Add(1)
		responseNonce := bytes.Repeat([]byte{5}, 12)
		response := gcm.Seal(responseNonce, responseNonce, []byte(`{"ok":true}`), append([]byte("echo-home-response-1"), b[:12]...))
		io.WriteString(w, base64.StdEncoding.EncodeToString(response))
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d := &Display{r: testRenderer(), poke: make(chan struct{}, 1)}
	d.standby.ctx = ctx
	d.standby.cfg = homeConfig{Key: hexutil.EncodeToString(key), URL: server.URL, Scenes: []homeScene{{"dim", "照明を暗く", "mdi:brightness-4"}, {"night", "寝る直前", "mdi:bed"}}}
	d.standby.view = standbyView{Enabled: true, Scenes: d.standby.cfg.Scenes}
	d.r.drawStandby(scene{phase: "idle", now: time.Now(), home: d.standby.view})
	b := d.r.homeZones[1].bounds
	at := b.Min.Add(b.Size().Div(2))
	if !d.tapStandby(at) || !d.tapStandby(at) {
		t.Fatal("rendered button missed")
	}
	deadline := time.Now().Add(3 * time.Second)
	for d.standby.snapshot().Status != "寝る直前：受付済み" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if calls.Load() != 1 || d.standby.snapshot().Status != "寝る直前：受付済み" {
		t.Fatal("tap not acknowledged exactly once")
	}
	d.r.draw(scene{phase: "thinking", now: time.Now()})
	if d.tapStandby(at) {
		t.Fatal("invisible scene was actionable")
	}
}
