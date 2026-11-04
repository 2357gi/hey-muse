package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

func TestRoundTripJapanese(t *testing.T) {
	a, b := net.Pipe()
	defer b.Close()
	b.SetDeadline(time.Now().Add(3 * time.Second))
	pcm := bytes.Repeat([]byte{1, 2}, 3200)
	go handle(a, func(p []byte) (string, error) {
		if !bytes.Equal(p, pcm) {
			t.Error("PCM changed")
		}
		return "日本語で短く挨拶して。", nil
	}, "tiny-ja")
	sendEvent(b, "transcribe", map[string]any{"language": "ja"})
	f := map[string]any{"rate": 16000, "width": 2, "channels": 1}
	sendEvent(b, "audio-start", f)
	h, _ := json.Marshal(event{Type: "audio-chunk", Data: f, PayloadLength: len(pcm)})
	b.Write(append(h, '\n'))
	b.Write(pcm)
	sendEvent(b, "audio-stop", nil)
	e, _, err := readEvent(bufio.NewReader(b))
	if err != nil || e.Type != "transcript" || e.Data["text"] != "日本語で短く挨拶して。" {
		t.Fatalf("%+v %v", e, err)
	}
}
func TestRejectUnboundedFrames(t *testing.T) {
	for _, s := range []string{"{\"type\":\"audio-chunk\",\"payload_length\":960001}\n", "{\"type\":\"audio-chunk\",\"data_length\":-1}\n", strings.Repeat("x", maxHeader+1) + "\n"} {
		_, _, err := readEvent(bufio.NewReaderSize(strings.NewReader(s), maxHeader+1))
		if err == nil {
			t.Fatal("accepted invalid frame")
		}
	}
}
func TestMalformedFormatNeverReachesWorker(t *testing.T) {
	a, b := net.Pipe()
	defer b.Close()
	b.SetDeadline(time.Now().Add(3 * time.Second))
	go handle(a, func([]byte) (string, error) { t.Error("worker called"); return "", nil }, "tiny")
	sendEvent(b, "transcribe", map[string]any{"language": "ja"})
	sendEvent(b, "audio-start", map[string]any{"rate": 44100, "width": 2, "channels": 1})
	e, _, err := readEvent(bufio.NewReader(b))
	if err != nil || e.Type != "error" {
		t.Fatal(e, err)
	}
}
func TestPartialPayloadFails(t *testing.T) {
	_, _, err := readEvent(bufio.NewReader(strings.NewReader("{\"type\":\"audio-chunk\",\"payload_length\":4}\nab")))
	if err != io.ErrUnexpectedEOF {
		t.Fatal(err)
	}
}
