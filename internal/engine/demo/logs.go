package demo

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

type logLine struct {
	time  time.Time
	level string
	msg   string
}

type logBuffer struct {
	mu    sync.Mutex
	lines []logLine
	subs  map[int]chan logLine
	next  int
	max   int
}

func newLogBuffer(max int) *logBuffer {
	return &logBuffer{subs: map[int]chan logLine{}, max: max}
}

func (b *logBuffer) add(level, msg string) {
	b.mu.Lock()
	line := logLine{time: time.Now(), level: level, msg: msg}
	b.lines = append(b.lines, line)
	if len(b.lines) > b.max {
		b.lines = b.lines[len(b.lines)-b.max:]
	}
	subs := make([]chan logLine, 0, len(b.subs))
	for _, ch := range b.subs {
		subs = append(subs, ch)
	}
	b.mu.Unlock()
	for _, ch := range subs {
		select {
		case ch <- line:
		default:
		}
	}
}

func (b *logBuffer) subscribe() (int, chan logLine) {
	b.mu.Lock()
	defer b.mu.Unlock()
	id := b.next
	b.next++
	ch := make(chan logLine, 256)
	b.subs[id] = ch
	return id, ch
}

func (b *logBuffer) unsubscribe(id int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if ch, ok := b.subs[id]; ok {
		delete(b.subs, id)
		close(ch)
	}
}

func (b *logBuffer) snapshot(tail int, timestamps bool) io.ReadCloser {
	b.mu.Lock()
	defer b.mu.Unlock()
	lines := b.lines
	if tail > 0 && len(lines) > tail {
		lines = lines[len(lines)-tail:]
	}
	var buf bytes.Buffer
	for _, l := range lines {
		if timestamps {
			buf.WriteString(l.time.Format(time.RFC3339) + " ")
		}
		fmt.Fprintf(&buf, "[%s] %s\n", l.level, l.msg)
	}
	return io.NopCloser(&buf)
}

func (cs *containerState) seedLogs(name, state string) {
	start := time.Now().Add(-2 * time.Hour)
	common := []string{
		"starting service " + name,
		"configuration loaded from /etc/config.yaml",
		"listening on 0.0.0.0:8080",
		"connected to database pool (size=10)",
		"health check passed",
	}
	for i, msg := range common {
		cs.logs.lines = append(cs.logs.lines, logLine{
			time:  start.Add(time.Duration(i) * 7 * time.Minute),
			level: "INFO",
			msg:   msg,
		})
	}
	if state == "exited" {
		cs.logs.lines = append(cs.logs.lines, logLine{
			time:  start.Add(time.Hour),
			level: "ERROR",
			msg:   "graceful shutdown failed: context deadline exceeded",
		})
		cs.logs.lines = append(cs.logs.lines, logLine{
			time:  start.Add(time.Hour + time.Second),
			level: "INFO",
			msg:   "server stopped",
		})
		return
	}
	cs.logs.lines = append(cs.logs.lines,
		logLine{time: start.Add(58 * time.Minute), level: "WARN", msg: "slow query detected (142ms): SELECT * FROM orders WHERE status='open'"},
		logLine{time: start.Add(59 * time.Minute), level: "INFO", msg: "request completed status=200 path=/api/health duration=3ms"},
		logLine{time: start.Add(60 * time.Minute), level: "INFO", msg: "request completed status=200 path=/api/products duration=18ms"},
	)
}

func formatLogLine(l logLine, timestamps bool) string {
	var b strings.Builder
	if timestamps {
		b.WriteString(l.time.Format(time.RFC3339) + " ")
	}
	fmt.Fprintf(&b, "[%s] %s\n", l.level, l.msg)
	return b.String()
}
