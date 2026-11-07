package demo

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

type demoTerminal struct {
	mu     sync.Mutex
	buf    []byte
	line   []byte
	closed bool
	notify chan struct{}
	host   string
	cwd    string
	err    error
}

func newDemoTerminal(name string) *demoTerminal {
	t := &demoTerminal{
		notify: make(chan struct{}, 16),
		host:   name,
		cwd:    "/",
	}
	t.echo("\x1b[1;32mdockerview demo shell\x1b[0m\r\n")
	t.echo("Docker daemon is unavailable, so this is a simulated shell.\r\n")
	t.echo("Try: ls, pwd, cat /etc/os-release, env, ps, uname -a, help\r\n\r\n")
	t.prompt()
	return t
}

func (t *demoTerminal) prompt() {
	t.echo("\x1b[1;34mroot@" + t.host + "\x1b[0m:\x1b[1;36m" + t.cwd + "\x1b[0m# ")
}

func (t *demoTerminal) echo(s string) {
	t.buf = append(t.buf, []byte(s)...)
	select {
	case t.notify <- struct{}{}:
	default:
	}
}

func (t *demoTerminal) Read(p []byte) (int, error) {
	for {
		t.mu.Lock()
		if len(t.buf) > 0 {
			n := copy(p, t.buf)
			t.buf = t.buf[n:]
			t.mu.Unlock()
			return n, nil
		}
		if t.closed {
			err := t.err
			t.mu.Unlock()
			if err == nil {
				err = fmt.Errorf("terminal closed")
			}
			return 0, err
		}
		t.mu.Unlock()
		<-t.notify
	}
}

func (t *demoTerminal) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, r := range string(p) {
		switch r {
		case '\r', '\n':
			t.echo("\r\n")
			cmd := strings.TrimSpace(string(t.line))
			t.line = nil
			t.execute(cmd)
			t.prompt()
		case 127, 8:
			if len(t.line) > 0 {
				t.line = t.line[:len(t.line)-1]
				t.echo("\b \b")
			}
		case 3:
			t.echo("^C\r\n")
			t.line = nil
			t.prompt()
		case 4:
			t.echo("logout\r\n")
			t.closed = true
			t.err = fmt.Errorf("terminal closed")
		default:
			if r >= 32 {
				t.line = append(t.line, byte(r))
				t.echo(string(r))
			}
		}
	}
	return len(p), nil
}

func (t *demoTerminal) execute(cmd string) {
	fields := strings.Fields(cmd)
	if len(fields) == 0 {
		return
	}
	switch fields[0] {
	case "help":
		t.echo("Simulated commands: ls, pwd, cd, cat, env, ps, uname, whoami, date, echo, clear, exit\r\n")
	case "ls":
		t.echo("bin  dev  etc  home  proc  root  sys  tmp  usr  var\r\n")
	case "pwd":
		t.echo(t.cwd + "\r\n")
	case "cd":
		dir := "/"
		if len(fields) > 1 {
			dir = fields[1]
		}
		if dir == ".." && t.cwd != "/" {
			idx := strings.LastIndex(strings.TrimSuffix(t.cwd, "/"), "/")
			if idx <= 0 {
				t.cwd = "/"
			} else {
				t.cwd = t.cwd[:idx]
			}
		} else if strings.HasPrefix(dir, "/") {
			t.cwd = dir
		} else if dir != "." {
			t.cwd = strings.TrimSuffix(t.cwd, "/") + "/" + dir
		}
	case "cat":
		if len(fields) < 2 {
			t.echo("cat: missing operand\r\n")
			return
		}
		if fields[1] == "/etc/os-release" {
			t.echo("NAME=\"Alpine Linux\"\r\nID=alpine\r\nVERSION_ID=3.20.3\r\nPRETTY_NAME=\"Alpine Linux v3.20\"\r\n")
			return
		}
		if fields[1] == "/etc/hostname" {
			t.echo(t.host + "\r\n")
			return
		}
		t.echo("cat: " + fields[1] + ": No such file or directory\r\n")
	case "env":
		t.echo("PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin\r\nHOSTNAME=" + t.host + "\r\nHOME=/root\r\n")
	case "ps":
		fmt.Fprintf(bufferWriter{t}, "PID   USER     TIME  COMMAND\r\n    1 root      0:00 /sbin/tini -- /entrypoint.sh\r\n   12 root      0:02 server\r\n")
	case "uname":
		t.echo("Linux " + t.host + " 6.6.0-demo #1 SMP x86_64 GNU/Linux\r\n")
	case "whoami":
		t.echo("root\r\n")
	case "date":
		t.echo(time.Now().Format(time.RFC1123) + "\r\n")
	case "echo":
		t.echo(strings.Join(fields[1:], " ") + "\r\n")
	case "clear":
		t.echo("\x1b[2J\x1b[H")
	case "exit", "logout":
		t.echo("logout\r\n")
		t.closed = true
		t.err = fmt.Errorf("terminal closed")
	default:
		t.echo(fields[0] + ": command not found (demo shell)\r\n")
	}
}

type bufferWriter struct{ t *demoTerminal }

func (w bufferWriter) Write(p []byte) (int, error) {
	w.t.echo(string(p))
	return len(p), nil
}

func (t *demoTerminal) Resize(cols, rows uint) error { return nil }

func (t *demoTerminal) Close() error {
	t.mu.Lock()
	t.closed = true
	t.mu.Unlock()
	select {
	case t.notify <- struct{}{}:
	default:
	}
	return nil
}
