// Loopback-only Wyoming adapter for the persistent on-device Moonshine worker.
package main

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const maxAudio = 30 * 16000 * 2
const maxHeader = 16384

type event struct {
	Type          string         `json:"type"`
	Data          map[string]any `json:"data,omitempty"`
	DataLength    int            `json:"data_length,omitempty"`
	PayloadLength int            `json:"payload_length,omitempty"`
}

func readEvent(r *bufio.Reader) (event, []byte, error) {
	var e event
	line, err := r.ReadSlice('\n')
	if err != nil {
		return e, nil, err
	}
	if len(line) > maxHeader {
		return e, nil, errors.New("header too large")
	}
	if err = json.Unmarshal(line, &e); err != nil {
		return e, nil, err
	}
	if e.DataLength < 0 || e.DataLength > maxHeader || e.PayloadLength < 0 || e.PayloadLength > maxAudio {
		return e, nil, errors.New("event too large")
	}
	if e.DataLength > 0 {
		b := make([]byte, e.DataLength)
		if _, err = io.ReadFull(r, b); err != nil {
			return e, nil, err
		}
		if err = json.Unmarshal(b, &e.Data); err != nil {
			return e, nil, err
		}
	}
	p := make([]byte, e.PayloadLength)
	_, err = io.ReadFull(r, p)
	return e, p, err
}
func sendEvent(w io.Writer, kind string, data map[string]any) error {
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	h, err := json.Marshal(event{Type: kind, DataLength: len(b)})
	if err != nil {
		return err
	}
	_, err = w.Write(append(append(h, '\n'), b...))
	return err
}
func validFormat(d map[string]any) bool {
	return d["rate"] == float64(16000) && d["width"] == float64(2) && d["channels"] == float64(1)
}

type worker struct {
	cmd  *exec.Cmd
	in   io.WriteCloser
	out  *bufio.Reader
	busy chan struct{}
}

func startWorker(root, model string, threads int) (*worker, error) {
	cmd := exec.Command(filepath.Join(root, "glibc/ld-linux-aarch64.so.1"), "--library-path", root+"/glibc:"+root+"/lib", root+"/asr-worker", model, fmt.Sprint(threads))
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM}
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err = cmd.Start(); err != nil {
		return nil, err
	}
	w := &worker{cmd: cmd, in: in, out: bufio.NewReaderSize(out, 32768), busy: make(chan struct{}, 1)}
	done := make(chan error, 1)
	go func() {
		line, e := w.out.ReadString('\n')
		if e == nil && line != "READY\n" {
			e = errors.New("bad worker greeting")
		}
		done <- e
	}()
	select {
	case err = <-done:
	case <-time.After(45 * time.Second):
		err = errors.New("worker startup timeout")
	}
	if err != nil {
		cmd.Process.Kill()
		cmd.Wait()
		return nil, err
	}
	return w, nil
}
func (w *worker) close() { w.in.Close(); w.cmd.Process.Kill(); w.cmd.Wait() }
func (w *worker) recognize(p []byte) (string, error) {
	if len(p) < 3200 {
		return "", nil
	}
	peak := int32(0)
	for i := 0; i+1 < len(p); i += 2 {
		v := int32(int16(binary.LittleEndian.Uint16(p[i:])))
		if v < 0 {
			v = -v
		}
		if v > peak {
			peak = v
		}
	}
	if peak < 33 {
		return "", nil
	}
	select {
	case w.busy <- struct{}{}:
	default:
		return "", errors.New("busy")
	}
	defer func() { <-w.busy }()
	type answer struct {
		text string
		err  error
	}
	done := make(chan answer, 1)
	go func() {
		err := binary.Write(w.in, binary.LittleEndian, uint32(len(p)))
		if err == nil {
			_, err = w.in.Write(p)
		}
		var n uint32
		if err == nil {
			err = binary.Read(w.out, binary.LittleEndian, &n)
		}
		if n > maxHeader {
			err = errors.New("worker result too large")
		}
		var b []byte
		if err == nil {
			b = make([]byte, n)
			_, err = io.ReadFull(w.out, b)
		}
		done <- answer{strings.TrimSpace(string(b)), err}
	}()
	select {
	case a := <-done:
		if a.err != nil {
			log.Print("inference worker failed")
			os.Exit(2)
		}
		return a.text, a.err
	case <-time.After(10500 * time.Millisecond):
		log.Print("inference timeout; restarting service")
		w.cmd.Process.Kill()
		os.Exit(2)
	}
	return "", errors.New("worker unavailable")
}
func handle(c net.Conn, recognize func([]byte) (string, error), model string) {
	defer c.Close()
	c.SetDeadline(time.Now().Add(45 * time.Second))
	r := bufio.NewReaderSize(c, maxHeader+1)
	var pcm []byte
	requested, started := false, false
	fail := func() {
		sendEvent(c, "error", map[string]any{"code": "recognition-failed", "text": "Speech recognition failed"})
	}
	for {
		e, p, err := readEvent(r)
		if err != nil {
			if !errors.Is(err, io.EOF) {
				fail()
			}
			return
		}
		switch e.Type {
		case "describe":
			sendEvent(c, "info", map[string]any{"asr": []any{map[string]any{"name": "echo-moonshine-ja", "description": "On-device Japanese speech recognition", "installed": true, "version": "1", "models": []any{map[string]any{"name": model, "installed": true, "languages": []string{"ja"}}}}}})
			return
		case "transcribe":
			if requested || started {
				fail()
				return
			}
			lang, _ := e.Data["language"].(string)
			if lang != "" && lang != "ja" && lang != "ja-JP" {
				fail()
				return
			}
			requested = true
		case "audio-start":
			if !requested || started || !validFormat(e.Data) {
				fail()
				return
			}
			started = true
		case "audio-chunk":
			if !started || !validFormat(e.Data) || len(p)%2 != 0 || len(pcm)+len(p) > maxAudio {
				fail()
				return
			}
			pcm = append(pcm, p...)
		case "audio-stop":
			if !started {
				fail()
				return
			}
			text, err := recognize(pcm)
			if err != nil {
				fail()
				return
			}
			sendEvent(c, "transcript", map[string]any{"text": text, "language": "ja"})
			return
		default:
			fail()
			return
		}
	}
}
func main() {
	root := flag.String("root", "/data/echo-asr", "runtime directory")
	model := flag.String("model", "model", "model directory relative to root")
	threads := flag.Int("threads", 2, "inference CPU threads")
	port := flag.Int("port", 50024, "loopback Wyoming port")
	wav := flag.String("wav", "", "benchmark a 16 kHz mono PCM WAV and exit")
	check := flag.Bool("check", false, "check the existing loopback service; optionally send -wav")
	flag.Parse()
	if *check {
		if err := checkService(*port, *wav); err != nil {
			log.Fatal(err)
		}
		return
	}
	w, err := startWorker(*root, filepath.Join(*root, *model), *threads)
	if err != nil {
		log.Fatal(err)
	}
	defer w.close()
	if *wav != "" {
		b, e := os.ReadFile(*wav)
		if e != nil {
			log.Fatal(e)
		}
		pcm, e := wavePCM(b)
		if e != nil {
			log.Fatal(e)
		}
		for i := 0; i < 2; i++ {
			start := time.Now()
			t, e := w.recognize(pcm)
			if e != nil {
				log.Fatal(e)
			}
			json.NewEncoder(os.Stdout).Encode(map[string]any{"text": t, "seconds": time.Since(start).Seconds(), "audio_seconds": float64(len(pcm)) / 32000})
		}
		return
	}
	listener, err := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", *port))
	if err != nil {
		log.Fatal(err)
	}
	defer listener.Close()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
	go func() { <-sig; listener.Close() }()
	log.Printf("ready address=%s model=%s threads=%d", listener.Addr(), *model, *threads)
	slots := make(chan struct{}, 3)
	for {
		c, e := listener.Accept()
		if e != nil {
			return
		}
		select {
		case slots <- struct{}{}:
			go func() { defer func() { <-slots }(); handle(c, w.recognize, *model) }()
		default:
			c.Close()
		}
	}
}

func checkService(port int, path string) error {
	c, err := net.DialTimeout("tcp4", fmt.Sprintf("127.0.0.1:%d", port), 3*time.Second)
	if err != nil {
		return err
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(15 * time.Second))
	start := time.Now()
	if path == "" {
		err = sendEvent(c, "describe", nil)
	} else {
		b, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		pcm, e := wavePCM(b)
		if e != nil {
			return e
		}
		if e = sendEvent(c, "transcribe", map[string]any{"language": "ja"}); e != nil {
			return e
		}
		format := map[string]any{"rate": 16000, "width": 2, "channels": 1}
		if e = sendEvent(c, "audio-start", format); e != nil {
			return e
		}
		h, _ := json.Marshal(event{Type: "audio-chunk", Data: format, PayloadLength: len(pcm)})
		if _, e = c.Write(append(h, '\n')); e != nil {
			return e
		}
		if _, e = c.Write(pcm); e != nil {
			return e
		}
		err = sendEvent(c, "audio-stop", nil)
	}
	if err != nil {
		return err
	}
	e, _, err := readEvent(bufio.NewReaderSize(c, maxHeader+1))
	if err != nil {
		return err
	}
	if e.Type != "info" && e.Type != "transcript" {
		return fmt.Errorf("recognizer returned %s", e.Type)
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"event": e.Type, "data": e.Data, "seconds": time.Since(start).Seconds()})
}
func wavePCM(b []byte) ([]byte, error) {
	if len(b) < 12 || string(b[:4]) != "RIFF" || string(b[8:12]) != "WAVE" {
		return nil, errors.New("not WAV")
	}
	valid := false
	for pos := 12; pos+8 <= len(b); {
		n := int(binary.LittleEndian.Uint32(b[pos+4:]))
		start := pos + 8
		if n < 0 || n > len(b)-start {
			return nil, errors.New("truncated WAV")
		}
		chunk := b[start : start+n]
		switch string(b[pos : pos+4]) {
		case "fmt ":
			valid = len(chunk) >= 16 && binary.LittleEndian.Uint16(chunk) == 1 && binary.LittleEndian.Uint16(chunk[2:]) == 1 && binary.LittleEndian.Uint32(chunk[4:]) == 16000 && binary.LittleEndian.Uint16(chunk[14:]) == 16
		case "data":
			if !valid || n > maxAudio || n%2 != 0 {
				return nil, errors.New("invalid WAV format")
			}
			return chunk, nil
		}
		pos = start + n + n%2
	}
	return nil, errors.New("no WAV data")
}
