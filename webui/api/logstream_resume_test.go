package api

import (
	"bufio"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type logStreamFrame struct{ event, id, data string }

func readLogStreamFrame(t *testing.T, s *bufio.Scanner) logStreamFrame {
	t.Helper()
	f := logStreamFrame{}
	for s.Scan() {
		line := s.Text()
		if line == "" {
			if f.data != "" {
				return f
			}
			continue
		}
		k, v, _ := strings.Cut(line, ":")
		v = strings.TrimPrefix(v, " ")
		switch k {
		case "event":
			f.event = v
		case "id":
			f.id = v
		case "data":
			f.data = v
		}
	}
	t.Fatalf("stream ended: %v", s.Err())
	return f
}
func emitStreamLogs(t *testing.T, b *LogBroadcaster, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		if err := b.Handle(context.Background(), slog.NewRecord(time.Unix(0, 0), slog.LevelInfo, "identical", 0)); err != nil {
			t.Fatal(err)
		}
	}
}
func connectLogStream(t *testing.T, b *LogBroadcaster, cursor string) (*bufio.Scanner, func()) {
	t.Helper()
	d := &Deps{LogBroadcaster: b}
	mux := http.NewServeMux()
	d.Register(mux)
	srv := httptest.NewServer(mux)
	req, err := http.NewRequest("GET", srv.URL+"/api/logs/stream", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Last-Event-ID", cursor)
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		srv.Close()
		t.Fatal(err)
	}
	var once sync.Once
	closeStream := func() {
		once.Do(func() {
			if err := resp.Body.Close(); err != nil {
				t.Error(err)
			}
			srv.Close()
		})
	}
	t.Cleanup(closeStream)
	if resp.StatusCode != http.StatusOK {
		t.Fatal(resp.Status)
	}
	return bufio.NewScanner(resp.Body), closeStream
}
func streamSequence(t *testing.T, id string) uint64 {
	t.Helper()
	_, raw, ok := strings.Cut(id, ":")
	n, err := strconv.ParseUint(raw, 10, 64)
	if !ok || err != nil {
		t.Fatalf("invalid ID %q", id)
	}
	return n
}
func TestLogStreamNoReplayOnLatest(t *testing.T) {
	b := NewLogBroadcaster("info")
	emitStreamLogs(t, b, 3)
	s, close1 := connectLogStream(t, b, "")
	f := readLogStreamFrame(t, s)
	if f.event == "reset" {
		f = readLogStreamFrame(t, s)
	}
	streamSequence(t, f.id)
	for i := 2; i <= 3; i++ {
		f = readLogStreamFrame(t, s)
	}
	last := f.id
	close1()
	s, close2 := connectLogStream(t, b, last)
	defer close2()
	emitStreamLogs(t, b, 1)
	f = readLogStreamFrame(t, s)
	if f.event != "" || streamSequence(t, f.id) != 4 {
		t.Fatalf("latest cursor replayed old log: %+v", f)
	}
}
