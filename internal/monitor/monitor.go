package monitor

import (
	"context"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
	gnet "github.com/shirou/gopsutil/v4/net"

	"dockerview/internal/model"
)

type Monitor struct {
	mu       sync.Mutex
	lastNet  map[string]gnet.IOCountersStat
	lastTime time.Time
	cpuModel string
	cpuCores int
}

func New() *Monitor {
	m := &Monitor{lastNet: map[string]gnet.IOCountersStat{}}
	if infos, err := cpu.Info(); err == nil && len(infos) > 0 {
		m.cpuModel = strings.TrimSpace(infos[0].ModelName)
	}
	if count, err := cpu.Counts(true); err == nil {
		m.cpuCores = count
	}
	return m
}

func (m *Monitor) Snapshot(ctx context.Context) (model.HostStats, error) {
	now := time.Now()
	stats := model.HostStats{Timestamp: now, CPUModel: m.cpuModel, CPUCores: m.cpuCores}

	if percent, err := cpu.Percent(0, false); err == nil && len(percent) > 0 {
		stats.CPUPercent = percent[0]
	}
	if perCore, err := cpu.Percent(0, true); err == nil {
		stats.CPUPerCore = perCore
		if stats.CPUCores == 0 {
			stats.CPUCores = len(perCore)
		}
	}
	if avg, err := load.Avg(); err == nil {
		stats.Load1 = avg.Load1
		stats.Load5 = avg.Load5
		stats.Load15 = avg.Load15
	}
	if vm, err := mem.VirtualMemory(); err == nil {
		stats.MemTotal = vm.Total
		stats.MemUsed = vm.Used
		stats.MemFree = vm.Free
		stats.MemAvailable = vm.Available
		stats.MemPercent = vm.UsedPercent
	}
	if sm, err := mem.SwapMemory(); err == nil {
		stats.SwapTotal = sm.Total
		stats.SwapUsed = sm.Used
	}
	if up, err := host.Uptime(); err == nil {
		stats.Uptime = up
	}
	if boot, err := host.BootTime(); err == nil {
		stats.BootTime = boot
	}
	if hi, err := host.Info(); err == nil {
		stats.Hostname = hi.Hostname
		stats.Platform = strings.TrimSpace(hi.Platform + " " + hi.PlatformVersion)
		stats.Kernel = hi.KernelVersion
		stats.Processes = hi.Procs
	}
	if stats.Hostname == "" {
		stats.Hostname, _ = os.Hostname()
	}

	stats.Disks = m.disks(ctx)
	stats.Networks, stats.NetRxRate, stats.NetTxRate = m.networks(now)
	return stats, nil
}

func (m *Monitor) disks(ctx context.Context) []model.DiskStats {
	parts, err := disk.Partitions(false)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	out := make([]model.DiskStats, 0, len(parts))
	for _, p := range parts {
		if seen[p.Mountpoint] {
			continue
		}
		switch p.Fstype {
		case "proc", "sysfs", "tmpfs", "devtmpfs", "devpts", "cgroup", "cgroup2", "overlay", "squashfs", "ramfs", "mqueue", "nsfs", "tracefs", "debugfs", "securityfs", "pstore", "bpf", "autofs", "configfs", "fusectl", "hugetlbfs", "binfmt_misc", "rpc_pipefs":
			continue
		}
		usage, err := disk.Usage(p.Mountpoint)
		if err != nil || usage.Total == 0 {
			continue
		}
		seen[p.Mountpoint] = true
		out = append(out, model.DiskStats{
			Device:      p.Device,
			Mountpoint:  p.Mountpoint,
			Fstype:      p.Fstype,
			Total:       usage.Total,
			Used:        usage.Used,
			Free:        usage.Free,
			UsedPercent: usage.UsedPercent,
		})
	}
	return out
}

func (m *Monitor) networks(now time.Time) ([]model.NetStats, float64, float64) {
	counters, err := gnet.IOCounters(true)
	if err != nil {
		return nil, 0, 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	elapsed := now.Sub(m.lastTime).Seconds()
	out := make([]model.NetStats, 0, len(counters))
	var totalRx, totalTx float64
	for _, c := range counters {
		if c.Name == "lo" {
			continue
		}
		ns := model.NetStats{Name: c.Name, RxBytes: c.BytesRecv, TxBytes: c.BytesSent}
		if prev, ok := m.lastNet[c.Name]; ok && elapsed > 0 {
			rx := float64(c.BytesRecv - prev.BytesRecv)
			tx := float64(c.BytesSent - prev.BytesSent)
			if rx >= 0 {
				ns.RxRate = rx / elapsed
				totalRx += ns.RxRate
			}
			if tx >= 0 {
				ns.TxRate = tx / elapsed
				totalTx += ns.TxRate
			}
		}
		out = append(out, ns)
		m.lastNet[c.Name] = c
	}
	m.lastTime = now
	return out, totalRx, totalTx
}
