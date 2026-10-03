package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestLogLevelVarImmediateEffect 日志级别更新后立即对使用同一 LevelVar 的 handler 生效（Build6 Step 4）
func TestLogLevelVarImmediateEffect(t *testing.T) {
	defer SetLogLevel("info")

	SetLogLevel("info")
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: LogLevelVar}))

	logger.Debug("debug-should-be-hidden")
	if buf.Len() != 0 {
		t.Errorf("info 级别不应输出 debug: %q", buf.String())
	}

	SetLogLevel("debug")
	logger.Debug("debug-should-be-visible")
	if !strings.Contains(buf.String(), "debug-should-be-visible") {
		t.Errorf("更新级别后 debug 应立即可见: %q", buf.String())
	}

	SetLogLevel("error")
	buf.Reset()
	logger.Info("info-should-be-hidden")
	if buf.Len() != 0 {
		t.Errorf("error 级别不应输出 info: %q", buf.String())
	}
	logger.Error("error-should-be-visible")
	if !strings.Contains(buf.String(), "error-should-be-visible") {
		t.Errorf("error 级别应输出 error: %q", buf.String())
	}
}

// TestParseLogLevel 业务设置字符串到 slog 级别的映射，未知值按 info 处理
func TestParseLogLevel(t *testing.T) {
	cases := map[string]slog.Level{
		"debug": slog.LevelDebug,
		"info":  slog.LevelInfo,
		"warn":  slog.LevelWarn,
		"error": slog.LevelError,
		"trace": slog.LevelInfo,
		"":      slog.LevelInfo,
	}
	for in, want := range cases {
		if got := ParseLogLevel(in); got != want {
			t.Errorf("ParseLogLevel(%q) = %v, want %v", in, got, want)
		}
	}
}

// TestLogLevelVarSharedByInitLogger InitLoggerWithBroadcaster 后全局默认 logger 使用可动态更新的级别
func TestLogLevelVarSharedByInitLogger(t *testing.T) {
	prev := slog.Default()
	defer func() {
		slog.SetDefault(prev)
		SetLogLevel("info")
	}()

	// extra 固定为不可用级别：让 MultiHandler.Enabled 只由 stdout 的 LogLevelVar 决定
	extra := slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError + 1})
	InitLoggerWithBroadcaster("warn", extra)
	if LogLevelVar.Level() != slog.LevelWarn {
		t.Fatalf("初始化后级别 = %v, want warn", LogLevelVar.Level())
	}
	if slog.Default().Enabled(t.Context(), slog.LevelInfo) {
		t.Error("warn 级别下 info 不应启用")
	}
	SetLogLevel("debug")
	if !slog.Default().Enabled(t.Context(), slog.LevelDebug) {
		t.Error("更新为 debug 后应立即对默认 logger 生效")
	}
}

type p313Handler struct {
	enabled bool
	fn      func(context.Context, slog.Record) error
}

func (h p313Handler) Enabled(context.Context, slog.Level) bool      { return h.enabled }
func (h p313Handler) Handle(c context.Context, r slog.Record) error { return h.fn(c, r) }
func (h p313Handler) WithAttrs([]slog.Attr) slog.Handler            { return h }
func (h p313Handler) WithGroup(string) slog.Handler                 { return h }

type p313Error struct{ id string }

func (e *p313Error) Error() string { return e.id }
func TestP313DispatchAndErrors(t *testing.T) {
	first := errors.New("first")
	second := &p313Error{"second"}
	cases := []struct {
		name      string
		enabled   []bool
		errs      []error
		wantCalls []int
	}{
		{"first_fails", []bool{true, true}, []error{first, nil}, []int{0, 1}},
		{"middle_fails", []bool{true, true, true}, []error{nil, first, nil}, []int{0, 1, 2}},
		{"last_fails", []bool{true, true}, []error{nil, first}, []int{0, 1}},
		{"multiple_fail", []bool{true, true, true}, []error{first, second, nil}, []int{0, 1, 2}},
		{"disabled_error", []bool{true, false, true}, []error{nil, first, nil}, []int{0, 2}},
		{"all_disabled", []bool{false, false}, []error{first, second}, nil},
		{"all_success", []bool{true, true}, []error{nil, nil}, []int{0, 1}},
		{"empty", nil, nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var calls []int
			var expected []error
			var hs []slog.Handler
			ctx, cancel := context.WithCancel(context.WithValue(context.Background(), struct{}{}, "context-marker"))
			cancel()
			r := slog.NewRecord(time.Unix(10, 0), slog.LevelWarn, "record-marker", 123)
			r.AddAttrs(slog.String("key", "value"))
			for i, on := range tc.enabled {
				if on && tc.errs[i] != nil {
					expected = append(expected, tc.errs[i])
				}
				hs = append(hs, p313Handler{on, func(c context.Context, got slog.Record) error {
					calls = append(calls, i)
					if c != ctx || got.Time != r.Time || got.Level != r.Level || got.Message != r.Message || got.PC != r.PC || got.NumAttrs() != 1 {
						t.Error("record or context changed")
					}
					got.Attrs(func(a slog.Attr) bool {
						if !a.Equal(slog.String("key", "value")) {
							t.Error("attribute changed")
						}
						return true
					})
					return tc.errs[i]
				}})
			}
			mh := slog.NewMultiHandler(hs...)
			err := mh.Handle(ctx, r)
			if !reflect.DeepEqual(calls, tc.wantCalls) {
				t.Errorf("calls=%v want=%v", calls, tc.wantCalls)
			}
			switch len(expected) {
			case 0:
				if err != nil {
					t.Errorf("want nil got %v", err)
				}
			case 1:
				if !errors.Is(err, expected[0]) {
					t.Errorf("single error identity lost: %T", err)
				}
			default:
				for _, e := range expected {
					if !errors.Is(err, e) {
						t.Errorf("missing error %v", e)
					}
				}
				var typed *p313Error
				if !errors.As(err, &typed) || typed != second {
					t.Error("typed error lost")
				}
				unwrapped, ok := err.(interface{ Unwrap() []error })
				if !ok || !reflect.DeepEqual(unwrapped.Unwrap(), expected) {
					t.Error("error order changed")
				}
			}
		})
	}
}
func TestP313RecordIsolation(t *testing.T) {
	r := slog.NewRecord(time.Time{}, slog.LevelInfo, "record", 0)
	// Add 为 overflow 属性预留容量，使未 Clone 的副本追加有判别力。
	r.Add("a", 1, "b", 2, "c", 3, "d", 4, "e", 5, "f", 6, slog.Group("empty"))
	var records []slog.Record
	var hs []slog.Handler
	for i := range 2 {
		hs = append(hs, p313Handler{true, func(_ context.Context, got slog.Record) error {
			got.AddAttrs(slog.Int("sink", i))
			records = append(records, got)
			return nil
		}})
	}
	if err := slog.NewMultiHandler(hs...).Handle(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	for i, got := range records {
		if got.NumAttrs() != 7 {
			t.Errorf("sink %d: attrs=%d want 7", i, got.NumAttrs())
		}
		got.Attrs(func(a slog.Attr) bool {
			if a.Key == "!BUG" {
				t.Errorf("sink %d: unsafe shared record", i)
			}
			if a.Key == "sink" && a.Value.Int64() != int64(i) {
				t.Errorf("sink %d: wrong marker %v", i, a)
			}
			return true
		})
	}
	if r.NumAttrs() != 6 {
		t.Error("original changed")
	}
}
func TestP313ConcurrentCalls(t *testing.T) {
	var calls atomic.Int64
	sentinel := errors.New("failure")
	mh := slog.NewMultiHandler(p313Handler{true, func(context.Context, slog.Record) error { return sentinel }}, p313Handler{true, func(context.Context, slog.Record) error { calls.Add(1); return nil }})
	var wg sync.WaitGroup
	for i := range 32 {
		wg.Go(func() {
			for j := range 50 {
				if err := mh.Handle(context.Background(), slog.NewRecord(time.Time{}, slog.LevelInfo, fmt.Sprint(i, j), 0)); !errors.Is(err, sentinel) {
					t.Error("identity changed")
				}
			}
		})
	}
	wg.Wait()
	if calls.Load() != 1600 {
		t.Errorf("calls=%d want=1600", calls.Load())
	}
}

// TestP313StdlibWiring 证明产品初始化入口确实采用标准库 Handler。
func TestP313StdlibWiring(t *testing.T) {
	previous := slog.Default()
	previousLevel := LogLevelVar.Level()
	defer func() { slog.SetDefault(previous); LogLevelVar.Set(previousLevel) }()
	InitLoggerWithBroadcaster("info", p313Handler{true, func(context.Context, slog.Record) error { return nil }})
	if _, ok := slog.Default().Handler().(*slog.MultiHandler); !ok {
		t.Fatalf("default handler = %T", slog.Default().Handler())
	}
}
