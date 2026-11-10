//go:build linux

package monitor

// Linux 进程采集：直接扫 /proc，每个进程只读一次 stat（外加一次目录 lstat 取属主 uid），
// 不依赖外部命令、不依赖 CGO，也不需要为每个进程建一个 gopsutil 对象。

import (
	"os"
	"path"
	"strconv"
	"strings"
	"syscall"
)

// clkTicks 是内核 USER_HZ：/proc/<pid>/stat 的 utime/stime 以它为 1 秒。
// Linux（含 arm64）上恒为 100，无需 sysconf。
const clkTicks = 100.0

func scanProcs() ([]procSample, int, int) {
	const procRoot = "/proc"
	ents, err := os.ReadDir(procRoot)
	if err != nil {
		// 没有 /proc（例如被极简容器裁掉）时返回空表，前端显示「无数据」。
		return nil, 0, 0
	}
	users := readPasswd("/etc/passwd")
	pageSize := float64(os.Getpagesize())

	out := make([]procSample, 0, len(ents))
	total, running := 0, 0
	for _, e := range ents {
		name := e.Name()
		// /proc 下只有纯数字目录是进程，跳过 self / net / sys 等。
		if name == "" || name[0] < '0' || name[0] > '9' {
			continue
		}
		pid, err := strconv.Atoi(name)
		if err != nil {
			continue
		}
		b, err := os.ReadFile(path.Join(procRoot, name, "stat"))
		if err != nil {
			// 进程刚退出属正常，跳过即可。
			continue
		}
		s, ticks, ok := parseProcStat(string(b), pid)
		if !ok || s.pid != pid {
			continue
		}
		s.cpuSeconds = float64(ticks) / clkTicks
		s.rss = uint64(float64(s.rss) * pageSize) // stat 的 rss 是页数
		if info, err := e.Info(); err == nil {
			if st, ok := info.Sys().(*syscall.Stat_t); ok {
				s.user = userOf(users, int(st.Uid))
			}
		}
		total++
		if s.state == "R" {
			running++
		}
		out = append(out, s)
	}
	return out, total, running
}

// procDetails 补读启动命令。用户名与线程数在扫描阶段已由 stat / 目录属主得到，不再重复读。
func procDetails(pid int) procDetail {
	b, err := os.ReadFile(path.Join("/proc", strconv.Itoa(pid), "cmdline"))
	if err != nil || len(b) == 0 {
		// 内核线程没有 cmdline，交给前端按 ps 惯例显示为 [comm]。
		return procDetail{}
	}
	return procDetail{cmdline: trimCmdline(b)}
}

// readPasswd 解析 /etc/passwd，返回 uid -> 用户名。
// 容器内这个文件是容器自己的，属于尽力而为：解析不到就回退成数字 uid。
func readPasswd(file string) map[int]string {
	out := map[int]string{}
	b, err := os.ReadFile(file)
	if err != nil {
		return out
	}
	for _, line := range strings.Split(string(b), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, ":")
		if len(f) < 3 {
			continue
		}
		uid, err := strconv.Atoi(f[2])
		if err != nil {
			continue
		}
		if _, dup := out[uid]; !dup {
			out[uid] = f[0]
		}
	}
	return out
}

func userOf(users map[int]string, uid int) string {
	if name, ok := users[uid]; ok {
		return name
	}
	return strconv.Itoa(uid)
}
