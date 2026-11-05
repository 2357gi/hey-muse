package mothership

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	hexutil "encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"
)

func HomeRequest(ctx context.Context, url, keyHex string, payload, out any) error {
	key, err := hexutil.DecodeString(keyHex)
	if err != nil {
		return err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	box, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	nonce := make([]byte, box.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return err
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	packet := []byte(base64.StdEncoding.EncodeToString(box.Seal(append([]byte(nil), nonce...), nonce, data, []byte("echo-home-request-1"))))
	mac := hmac.New(sha256.New, key)
	mac.Write(packet)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(packet))
	if err != nil {
		return err
	}
	req.Header.Set("X-Echo-Signature", hexutil.EncodeToString(mac.Sum(nil)))
	client := http.Client{Timeout: 95 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return errors.New("dashboard unavailable")
	}
	encoded, err := io.ReadAll(io.LimitReader(resp.Body, 1<<17))
	if err != nil {
		return err
	}
	body, err := base64.StdEncoding.DecodeString(string(encoded))
	if err != nil || len(body) < box.NonceSize()+box.Overhead() {
		return errors.New("invalid response")
	}
	plain, err := box.Open(nil, body[:12], body[12:], append([]byte("echo-home-response-1"), nonce...))
	if err != nil {
		return err
	}
	err = json.Unmarshal(plain, out)
	return err
}
