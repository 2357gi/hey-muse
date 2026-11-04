package endpoint

import (
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"
)

func TestNeuralStreamsSignedPCMAndHonorsCompletedSegment(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	n := &Neural{conn: a}
	done := make(chan error, 1)
	go func() {
		for turn := 0; turn < 2; turn++ {
			var size uint32
			if err := binary.Read(b, binary.LittleEndian, &size); err != nil {
				done <- err
				return
			}
			p := make([]byte, size)
			if _, err := io.ReadFull(b, p); err != nil {
				done <- err
				return
			}
			if size != 6 || int16(binary.LittleEndian.Uint16(p)) != -32768 || int16(binary.LittleEndian.Uint16(p[4:])) != 32767 {
				t.Error("PCM changed")
			}
			flags, end := uint32(1), uint32(0)
			if turn == 1 {
				flags = 2
				end = 4
			}
			binary.Write(b, binary.LittleEndian, []uint32{flags, end})
		}
		done <- nil
	}()
	samples := []int16{-32768, 0, 32767}
	if end, err := n.Feed(samples); err != nil || end {
		t.Fatal(end, err)
	}
	if end, err := n.Feed(samples); err != nil || !end || n.EndedAt() != 4 {
		t.Fatal(end, err, n.EndedAt())
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
func TestNeuralFailureDoesNotBlockMicrophone(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	n := &Neural{conn: a}
	start := time.Now()
	_, err := n.Feed(make([]int16, 320))
	if err == nil || time.Since(start) > time.Second {
		t.Fatal("unbounded detector wait", err)
	}
}
func TestNeuralRejectsInvalidEndpoint(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	go func() {
		p := make([]byte, 644)
		io.ReadFull(b, p)
		binary.Write(b, binary.LittleEndian, []uint32{2, 999999})
	}()
	n := &Neural{conn: a}
	if _, err := n.Feed(make([]int16, 320)); err == nil {
		t.Fatal("accepted endpoint beyond audio")
	}
}
