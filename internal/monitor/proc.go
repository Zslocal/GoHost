package monitor

// 进程榜：与 go_local_host 的进程模块同一套口径。
//   - CPU：相邻两次采样间消耗的 CPU 时间 / 真实时间窗口，100% = 1 个逻辑核（与 top 一致）；
//   - 内存：RSS / MemTotal，RSS 不含被换出的部分；
//   - 用 start 标识识别 PID 复用，复用后的进程不与旧进程做差分；
//   - 启动命令只对入选榜单的进程补读，避免为了展示字段把所有进程多读一遍。

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"dockerview/internal/model"
)

const (
	// defaultProcTopN 是 CPU / 内存两个榜单的长度。
	defaultProcTopN = 8
	// procWarmup 是首次采样多等的一个短窗口：没有参照帧时 CPU 差分全为 0，
	// 多采一帧可以让首屏就显示真实占用。
	procWarmup = 300 * time.Millisecond
	// cmdlineMaxRunes 是启动命令的截断长度，避免超长参数撑爆表格。
	cmdlineMaxRunes = 120
)

// procSample 是单个进程某一次采样的平台无关结果。
// cpuSeconds 为累计 CPU 时间（秒），start 为进程启动标识，rss 为常驻内存（字节）。
type procSample struct {
	pid        int
	name       string
	user       string
	state      string
	threads    int
	cpuSeconds float64
	start      uint64
	rss        uint64
}

// procDetail 是只为榜单进程补读的展示字段，各平台按自己的能力填。
type procDetail struct {
	user    string
	cmdline string
	threads int
}

// collectProcs 采样一次进程表并与上一帧差分，返回按 CPU / 常驻内存排序的两个榜单。
func (m *Monitor) collectProcs(topN int, memTotal uint64) model.ProcStats {
	if topN <= 0 {
		topN = defaultProcTopN
	}

	m.procMu.Lock()
	defer m.procMu.Unlock()

	startAt := time.Now()
	samples, total, running := scanProcs()

	prev, prevAt := m.procPrev, m.procPrevAt
	if prev == nil {
		// 首帧没有参照，先等一个短窗口再采一帧；采样失败就退回第一帧（CPU 全为 0）。
		time.Sleep(procWarmup)
		prev, prevAt = indexProcs(samples), startAt
		if again, againTotal, againRunning := scanProcs(); len(again) > 0 {
			samples, total, running = again, againTotal, againRunning
		}
	}
	m.procPrev, m.procPrevAt = indexProcs(samples), time.Now()

	stats := model.ProcStats{Total: total, Running: running, Scanned: len(samples)}
	if len(samples) == 0 {
		return stats
	}

	elapsed := m.procPrevAt.Sub(prevAt).Seconds()
	if elapsed < 0.05 {
		// 两次采样挤在一起时差分噪声极大，按下限兜底，避免算出离谱的百分比。
		elapsed = 0.05
	}

	rows := make([]model.ProcInfo, 0, len(samples))
	for _, s := range samples {
		info := model.ProcInfo{
			PID:     s.pid,
			Name:    s.name,
			User:    s.user,
			State:   s.state,
			RSS:     s.rss,
			Threads: s.threads,
		}
		// PID 复用检测：启动标识变了说明是新进程，不能拿它和旧进程比 CPU 时间。
		if old, ok := prev[s.pid]; ok && old.start == s.start {
			if used := s.cpuSeconds - old.cpuSeconds; used > 0 {
				info.CPU = round2(used / elapsed * 100)
			}
		}
		if memTotal > 0 {
			info.Mem = clampPercent(round2(float64(s.rss) / float64(memTotal) * 100))
		}
		rows = append(rows, info)
	}

	byCPU := make([]model.ProcInfo, len(rows))
	copy(byCPU, rows)
	sort.Slice(byCPU, func(i, j int) bool {
		if byCPU[i].CPU != byCPU[j].CPU {
			return byCPU[i].CPU > byCPU[j].CPU
		}
		if byCPU[i].RSS != byCPU[j].RSS {
			return byCPU[i].RSS > byCPU[j].RSS
		}
		return byCPU[i].PID < byCPU[j].PID
	})

	byMem := make([]model.ProcInfo, len(rows))
	copy(byMem, rows)
	sort.Slice(byMem, func(i, j int) bool {
		if byMem[i].RSS != byMem[j].RSS {
			return byMem[i].RSS > byMem[j].RSS
		}
		return byMem[i].PID < byMem[j].PID
	})

	stats.TopCPU = headProcs(byCPU, topN)
	stats.TopMem = headProcs(byMem, topN)
	enrichProcLists(stats.TopCPU, stats.TopMem)
	return stats
}

func indexProcs(list []procSample) map[int]procSample {
	out := make(map[int]procSample, len(list))
	for _, s := range list {
		out[s.pid] = s
	}
	return out
}

func headProcs(list []model.ProcInfo, n int) []model.ProcInfo {
	if len(list) > n {
		return list[:n]
	}
	return list
}

// enrichProcLists 给榜单补上用户名、线程数与启动命令。
// 两个榜单常常出现同一进程，这里按 pid 去重，每个进程只读一次。
// 这些字段对未上榜的进程没有意义（Windows 上取线程数单次就要 6ms），所以只补榜单。
func enrichProcLists(lists ...[]model.ProcInfo) {
	cache := make(map[int]procDetail)
	for _, list := range lists {
		for i := range list {
			d, ok := cache[list[i].PID]
			if !ok {
				d = procDetails(list[i].PID)
				cache[list[i].PID] = d
			}
			if list[i].User == "" {
				list[i].User = d.user
			}
			if list[i].Cmdline == "" {
				list[i].Cmdline = d.cmdline
			}
			if list[i].Threads == 0 {
				list[i].Threads = d.threads
			}
		}
	}
}

// trimCmdline 把 /proc/<pid>/cmdline 的 NUL 分隔参数还原成一行，并截断到 cmdlineMaxRunes。
func trimCmdline(b []byte) string {
	cmd := strings.TrimSpace(strings.ReplaceAll(string(b), "\x00", " "))
	if r := []rune(cmd); len(r) > cmdlineMaxRunes {
		// 截断后补省略号，方便前端判断「这不是完整命令」。
		return string(r[:cmdlineMaxRunes]) + "…"
	}
	return cmd
}

func clampPercent(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

func round2(v float64) float64 {
	return float64(int64(v*100+0.5)) / 100
}

// parseProcStat 解析 /proc/<pid>/stat，返回平台无关样本与累计 CPU tick（utime+stime）。
// comm 可能带空格与括号，因此以最后一个 ')' 为界，其后的 token 从 state（字段 3）开始，
// 字段 n 对应 tokens[n-3]。
func parseProcStat(line string, pid int) (procSample, uint64, bool) {
	open := strings.IndexByte(line, '(')
	closeIdx := strings.LastIndexByte(line, ')')
	if open < 0 || closeIdx < open {
		return procSample{}, 0, false
	}
	rest := strings.Fields(line[closeIdx+1:])
	if len(rest) < 22 {
		return procSample{}, 0, false
	}
	num := func(i int) uint64 {
		v, _ := strconv.ParseUint(rest[i], 10, 64)
		return v
	}
	threads, _ := strconv.Atoi(rest[17])
	rss, _ := strconv.ParseInt(rest[21], 10, 64)
	if rss < 0 {
		rss = 0
	}
	return procSample{
		pid:     pid,
		name:    line[open+1 : closeIdx],
		state:   rest[0],
		start:   num(19),
		threads: threads,
		rss:     uint64(rss),
	}, num(11) + num(12), true
}
