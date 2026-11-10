package monitor

import (
	"strings"
	"testing"
)

// 取自 /proc/<pid>/stat 的真实形态：comm 里带空格和冒号，末尾 rss 为负数的情况也有。
const sampleStat = "1234 (nginx: worker process) S 1 1234 1234 0 -1 4194560 2431 0 0 0 12 34 0 0 20 0 3 0 98765 123456789 4321 18446744073709551615 1 1 0 0 0 0 0 0 0 0 0 0 17 3 0 0 0 0 0"

func TestParseProcStat(t *testing.T) {
	s, ticks, ok := parseProcStat(sampleStat, 1234)
	if !ok {
		t.Fatal("parseProcStat 解析失败")
	}
	if s.pid != 1234 {
		t.Errorf("pid = %d, 期望 1234", s.pid)
	}
	if s.name != "nginx: worker process" {
		t.Errorf("name = %q, 期望带空格与冒号的 comm", s.name)
	}
	if s.state != "S" {
		t.Errorf("state = %q, 期望 S", s.state)
	}
	if s.threads != 3 {
		t.Errorf("threads = %d, 期望 3（字段 20）", s.threads)
	}
	if s.start != 98765 {
		t.Errorf("start = %d, 期望 98765（字段 22）", s.start)
	}
	if s.rss != 4321 {
		t.Errorf("rss = %d, 期望 4321（字段 24，页数）", s.rss)
	}
	// utime=12 + stime=34，单位是 tick
	if ticks != 46 {
		t.Errorf("ticks = %d, 期望 46（utime+stime）", ticks)
	}
}

func TestParseProcStatMalformed(t *testing.T) {
	cases := []string{
		"",
		"1234 no-parens S 1 2 3",
		"1234 (short) S 1 2",
	}
	for _, c := range cases {
		if _, _, ok := parseProcStat(c, 1234); ok {
			t.Errorf("畸形输入 %q 不应解析成功", c)
		}
	}
}

func TestParseProcStatNegativeRSS(t *testing.T) {
	// 僵尸进程的 rss 可能为负数，应归零而不是下溢成一个巨大的 uint64。
	line := strings.Replace(sampleStat, " 4321 ", " -8 ", 1)
	s, _, ok := parseProcStat(line, 1234)
	if !ok {
		t.Fatal("解析失败")
	}
	if s.rss != 0 {
		t.Errorf("rss = %d, 期望 0", s.rss)
	}
}

func TestTrimCmdline(t *testing.T) {
	raw := []byte("/usr/bin/java\x00-Xmx512m\x00-jar\x00/app.jar\x00")
	if got, want := trimCmdline(raw), "/usr/bin/java -Xmx512m -jar /app.jar"; got != want {
		t.Errorf("trimCmdline = %q, 期望 %q", got, want)
	}
	if got := trimCmdline(nil); got != "" {
		t.Errorf("空 cmdline 应返回空串，实际 %q", got)
	}
	long := trimCmdline([]byte(strings.Repeat("a", 300)))
	if r := []rune(long); len(r) != cmdlineMaxRunes+1 || r[len(r)-1] != '…' {
		t.Errorf("超长命令应截断到 %d 个字符并带省略号，实际长度 %d", cmdlineMaxRunes, len(r))
	}
}

func TestClampPercent(t *testing.T) {
	cases := map[float64]float64{-1: 0, 0: 0, 55.5: 55.5, 100: 100, 260: 100}
	for in, want := range cases {
		if got := clampPercent(in); got != want {
			t.Errorf("clampPercent(%v) = %v, 期望 %v", in, got, want)
		}
	}
}
