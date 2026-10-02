package api

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"
)

// TestLogStreamCursorMatrix 固定缓存衔接、实例切换与非法游标边界。
func TestLogStreamCursorMatrix(t *testing.T) {
	cases := []struct {
		name, cursor, reason string
		first, count         int
	}{
		{"initial", "", "initial", 6, 1000},
		{"oldest-minus-one", "%s:5", "", 6, 1000},
		{"oldest", "%s:6", "", 7, 999},
		{"expired", "%s:4", "history_expired", 6, 1000},
		{"latest", "%s:1005", "", 0, 0},
		{"ahead", "%s:1006", "invalid_cursor", 6, 1000},
		{"restart", "other:1005", "instance_changed", 6, 1000},
		{"malformed", "junk", "invalid_cursor", 6, 1000},
		{"negative", "%s:-1", "invalid_cursor", 6, 1000},
		{"leadingzero", "%s:05", "invalid_cursor", 6, 1000},
		{"overflow", "%s:18446744073709551616", "invalid_cursor", 6, 1000}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := NewLogBroadcaster("info")
			emitStreamLogs(t, b, 1005)
			cursor := tc.cursor
			if len(cursor) > 1 && cursor[:2] == "%s" {
				cursor = fmt.Sprintf(cursor, b.epoch)
			}
			ch, reason, baseline, cancel := b.Subscribe(cursor)
			defer cancel()
			if reason != tc.reason || baseline != 5 || len(ch) != tc.count {
				t.Fatalf("got reason=%s baseline=%d len=%d", reason, baseline, len(ch))
			}
			for i := 0; i < tc.count; i++ {
				f := <-ch
				if f.Seq != uint64(tc.first+i) {
					t.Fatal(f.Seq)
				}
			}
			emitStreamLogs(t, b, 1)
			if f := <-ch; f.Seq != 1006 {
				t.Fatal(f.Seq)
			}
		})
	}
}

// TestLogStreamResetDisconnectAndEmpty 完整 reset 后立即断线仍从基准续传。
func TestLogStreamResetDisconnectAndEmpty(t *testing.T) {
	b := NewLogBroadcaster("info")
	s, close1 := connectLogStream(t, b, "old:500")
	reset := readLogStreamFrame(t, s)
	if reset.event != "reset" || reset.data != "instance_changed" || streamSequence(t, reset.id) != 0 {
		t.Fatalf("bad reset %+v", reset)
	}
	close1()
	s, close2 := connectLogStream(t, b, reset.id)
	defer close2()
	emitStreamLogs(t, b, 1)
	f := readLogStreamFrame(t, s)
	if f.event != "" || streamSequence(t, f.id) != 1 {
		t.Fatalf("reset repeated %+v", f)
	}
	b2 := NewLogBroadcaster("info")
	emitStreamLogs(t, b2, 1005)
	s2, c2 := connectLogStream(t, b2, b2.epoch+":4")
	r := readLogStreamFrame(t, s2)
	if r.data != "history_expired" || streamSequence(t, r.id) != 5 {
		t.Fatal(r)
	}
	c2()
	s2, c3 := connectLogStream(t, b2, r.id)
	defer c3()
	f = readLogStreamFrame(t, s2)
	if f.event != "" || streamSequence(t, f.id) != 6 {
		t.Fatal(f)
	}
}

// TestLogStreamConcurrentReplayOrdering 历史与并发新日志必须完整且严格递增。
func TestLogStreamConcurrentReplayOrdering(t *testing.T) {
	b := NewLogBroadcaster("info")
	emitStreamLogs(t, b, 100)
	var wg sync.WaitGroup
	start := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < 200; i++ {
			if err := b.Handle(context.Background(), slog.NewRecord(time.Time{}, slog.LevelInfo, "concurrent", 0)); err != nil {
				t.Error(err)
			}
		}
	}()
	close(start)
	ch, reason, _, cancel := b.Subscribe("")
	defer cancel()
	wg.Wait()
	if reason != "initial" || len(ch) != 300 {
		t.Fatalf("reason=%s len=%d", reason, len(ch))
	}
	for i := 1; i <= 300; i++ {
		e := <-ch
		if e.Seq != uint64(i) {
			t.Fatalf("got %d want %d", e.Seq, i)
		}
	}
}

// TestLogStreamPartialReplayReconnect 部分回放后重连只补尚未收到的日志。
func TestLogStreamPartialReplayReconnect(t *testing.T) {
	b := NewLogBroadcaster("info")
	emitStreamLogs(t, b, 3)
	s, c := connectLogStream(t, b, "")
	readLogStreamFrame(t, s)
	first := readLogStreamFrame(t, s)
	if streamSequence(t, first.id) != 1 {
		t.Fatal(first)
	}
	c()
	s, c = connectLogStream(t, b, first.id)
	defer c()
	for i := 2; i <= 3; i++ {
		f := readLogStreamFrame(t, s)
		if f.event != "" || streamSequence(t, f.id) != uint64(i) {
			t.Fatal(f)
		}
	}
	emitStreamLogs(t, b, 1)
	f := readLogStreamFrame(t, s)
	if streamSequence(t, f.id) != 4 {
		t.Fatal(f)
	}
}
