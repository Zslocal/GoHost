//go:build !linux

package monitor

// 非 Linux 平台（Windows / macOS 等）没有 /proc，改用 gopsutil 的进程接口兜底。
// 口径与 Linux 实现保持一致：cpuSeconds 为累计 CPU 时间（秒），start 为启动时刻，
// rss 为常驻内存字节数。
//
// 注意：Windows 上 NumThreads() 单次约 6ms（要查进程信息），全量遍历会把 2 秒一次的
// 刷新拖到 1.6 秒以上，因此线程数和用户名都只对入选榜单的进程读取。

import (
	"github.com/shirou/gopsutil/v4/process"
)

func scanProcs() ([]procSample, int, int) {
	procs, err := process.Processes()
	if err != nil {
		return nil, 0, 0
	}
	out := make([]procSample, 0, len(procs))
	running := 0
	for _, p := range procs {
		times, err := p.Times()
		if err != nil {
			// 进程已退出或无权限，跳过。
			continue
		}
		s := procSample{pid: int(p.Pid), cpuSeconds: times.Total()}
		if name, err := p.Name(); err == nil {
			s.name = name
		}
		if mi, err := p.MemoryInfo(); err == nil && mi != nil {
			s.rss = mi.RSS
		}
		if st, err := p.Status(); err == nil && len(st) > 0 {
			s.state = st[0][:1]
			if s.state == "R" {
				running++
			}
		}
		if ct, err := p.CreateTime(); err == nil && ct > 0 {
			s.start = uint64(ct)
		}
		out = append(out, s)
	}
	return out, len(procs), running
}

func procDetails(pid int) procDetail {
	p, err := process.NewProcess(int32(pid))
	if err != nil {
		return procDetail{}
	}
	var d procDetail
	d.user, _ = p.Username()
	if cmd, err := p.Cmdline(); err == nil {
		d.cmdline = trimCmdline([]byte(cmd))
	}
	if n, err := p.NumThreads(); err == nil {
		d.threads = int(n)
	}
	return d
}
