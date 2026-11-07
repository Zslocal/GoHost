package demo

import (
	"io"
	"sync"
	"time"
)

type logStream struct {
	mu     sync.Mutex
	buf    []byte
	closed bool
	notify chan struct{}
	subID  int
	buf_   *logBuffer
}

func (b *logBuffer) stream(tail int, timestamps bool) io.ReadCloser {
	b.mu.Lock()
	lines := b.lines
	if tail > 0 && len(lines) > tail {
		lines = lines[len(lines)-tail:]
	}
	id, ch := b.subscribeLocked()
	b.mu.Unlock()

	s := &logStream{notify: make(chan struct{}, 64), subID: id, buf_: b}
	for _, l := range lines {
		s.buf = append(s.buf, []byte(formatLogLine(l, timestamps))...)
	}
	go func() {
		for l := range ch {
			s.mu.Lock()
			s.buf = append(s.buf, []byte(formatLogLine(l, timestamps))...)
			s.mu.Unlock()
			select {
			case s.notify <- struct{}{}:
			default:
			}
		}
	}()
	return s
}

func (b *logBuffer) subscribeLocked() (int, chan logLine) {
	id := b.next
	b.next++
	ch := make(chan logLine, 256)
	b.subs[id] = ch
	return id, ch
}

func (s *logStream) Read(p []byte) (int, error) {
	for {
		s.mu.Lock()
		if len(s.buf) > 0 {
			n := copy(p, s.buf)
			s.buf = s.buf[n:]
			s.mu.Unlock()
			return n, nil
		}
		if s.closed {
			s.mu.Unlock()
			return 0, io.EOF
		}
		s.mu.Unlock()
		select {
		case <-s.notify:
		case <-time.After(30 * time.Second):
			return 0, io.EOF
		}
	}
}

func (s *logStream) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.mu.Unlock()
	s.buf_.unsubscribe(s.subID)
	select {
	case s.notify <- struct{}{}:
	default:
	}
	return nil
}
