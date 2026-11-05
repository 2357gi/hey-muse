// Command measure-muse measures a paired device's live transport without printing chat text or credentials.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"sync"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/lib/muse"
	"github.com/google/uuid"
)

type timingOutput struct{}

func (timingOutput) Write(b []byte) (int, error) {
	var record map[string]any
	if json.Unmarshal(b, &record) == nil && record["msg"] == "muse latency" {
		_, err := os.Stdout.Write(b)
		return len(b), err
	}
	return len(b), nil
}

func main() {
	rounds := flag.Int("rounds", 2, "pairs of new-chat and follow-up measurements")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 8*time.Minute)
	defer cancel()
	online := make(chan struct{})
	var once sync.Once
	client, err := muse.New(muse.Config{
		Store:       muse.FileStore{Path: "/data/misc/techo5/muse.json"},
		DisplayName: "Echo Show 5",
		Version:     "latency-measurement",
		Logger:      slog.New(slog.NewJSONHandler(timingOutput{}, nil)),
		OnState: func(s muse.ConnState) {
			if s.Phase == muse.Online {
				once.Do(func() { close(online) })
			}
		},
	})
	if err != nil {
		fmt.Println("measurement initialization failed")
		os.Exit(1)
	}
	ended := make(chan struct{})
	go func() { defer close(ended); _ = client.Run(ctx) }()
	defer func() { cancel(); <-ended }()
	select {
	case <-online:
	case <-ctx.Done():
		fmt.Println("measurement connect timeout")
		return
	case <-ended:
		fmt.Println("measurement connection ended")
		return
	}
	for i := 0; i < *rounds; i++ {
		chat := uuid.NewString()
		for j := 0; j < 2; j++ {
			in := muse.Text("日本の首都を地名だけで答えてください。")
			in.SessionID = chat
			fmt.Printf("measurement round=%d follow_up=%t\n", i+1, j == 1)
			reply, err := client.Ask(ctx, in, nil)
			fmt.Printf("measurement result failed=%t nonempty=%t\n", err != nil, strings.TrimSpace(reply.Text) != "")
			if err != nil {
				return
			}
		}
	}
}
