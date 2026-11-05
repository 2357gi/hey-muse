// Command measure-hall-camera records coarse light levels for a bounded hardware test.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/hardware/camera"
	"golang.org/x/sys/unix"
)

func main() {
	duration := flag.Duration("duration", 90*time.Second, "sampling duration (at most five minutes)")
	flag.Parse()
	if *duration <= 0 || *duration > 5*time.Minute {
		os.Exit(2)
	}
	cam := camera.Get()
	release, err := cam.Acquire()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	frames := make(chan *camera.Frame, 2)
	stop := cam.Frames.Listen(func(f *camera.Frame) {
		select {
		case frames <- f:
		default:
		}
	})
	deadline := time.NewTimer(*duration)
	out := bufio.NewWriter(os.Stdout)
	defer func() {
		stop()
		release()
		out.Flush()
		// Allow the driver's five-second release timer to power the sensor down.
		time.Sleep(7 * time.Second)
	}()
	for {
		select {
		case <-deadline.C:
			return
		case f := <-frames:
			levels := f.Luma(32, 24)
			if len(levels) != 32*24 {
				continue
			}
			var boot unix.Timespec
			if err := unix.ClockGettime(unix.CLOCK_BOOTTIME, &boot); err != nil {
				return
			}
			t := float64(boot.Nano())/1e9 - time.Since(f.At).Seconds()
			fmt.Fprintf(out, "%.4f", t)
			var tiles [12]int
			for i, l := range levels {
				tiles[(i/32/8)*4+(i%32)/8] += int(l)
			}
			for _, v := range tiles {
				fmt.Fprintf(out, ",%.2f", float64(v)/64)
			}
			fmt.Fprintln(out)
			out.Flush()
		}
	}
}
