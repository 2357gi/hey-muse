package endpoint

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"time"
)

// Neural is a per-utterance connection to the local CPU speech detector.
// Failure is bounded so a missing or failed helper cannot stall microphone audio.
type Neural struct {
	conn    net.Conn
	endedAt int
	sent    int
}

func DialNeural(ctx context.Context, path string) (*Neural, error) {
	d := net.Dialer{Timeout: 150 * time.Millisecond}
	conn, err := d.DialContext(ctx, "unix", path)
	if err != nil {
		return nil, err
	}
	return &Neural{conn: conn}, nil
}
func (n *Neural) Close() error { return n.conn.Close() }
func (n *Neural) EndedAt() int { return n.endedAt }
func (n *Neural) Feed(samples []int16) (bool, error) {
	if n.endedAt > 0 {
		return true, nil
	}
	for len(samples) > 0 {
		count := min(len(samples), 1600)
		buf := make([]byte, 4+count*2)
		binary.LittleEndian.PutUint32(buf, uint32(count*2))
		for i, v := range samples[:count] {
			binary.LittleEndian.PutUint16(buf[4+2*i:], uint16(v))
		}
		n.conn.SetDeadline(time.Now().Add(250 * time.Millisecond))
		for len(buf) > 0 {
			written, err := n.conn.Write(buf)
			if err != nil {
				return false, err
			}
			if written == 0 {
				return false, io.ErrShortWrite
			}
			buf = buf[written:]
		}
		var response [8]byte
		if _, err := io.ReadFull(n.conn, response[:]); err != nil {
			return false, err
		}
		flags := binary.LittleEndian.Uint32(response[:])
		end := binary.LittleEndian.Uint32(response[4:])
		n.sent += count
		if flags&^uint32(3) != 0 || end > uint32(n.sent) {
			return false, fmt.Errorf("invalid local VAD response")
		}
		if flags&2 != 0 {
			if end == 0 {
				return false, fmt.Errorf("missing speech endpoint")
			}
			n.endedAt = int(end)
			return true, nil
		}
		samples = samples[count:]
	}
	return false, nil
}
