package model

import (
	"encoding/json"
	"time"
)

type Port struct {
	IP          string `json:"ip,omitempty"`
	PrivatePort uint16 `json:"privatePort"`
	PublicPort  uint16 `json:"publicPort,omitempty"`
	Type        string `json:"type"`
}

type Mount struct {
	Type        string `json:"type"`
	Source      string `json:"source"`
	Destination string `json:"destination"`
	Mode        string `json:"mode,omitempty"`
	RW          bool   `json:"rw"`
}

type Container struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Image    string            `json:"image"`
	ImageID  string            `json:"imageId,omitempty"`
	Command  string            `json:"command,omitempty"`
	Created  int64             `json:"created"`
	State    string            `json:"state"`
	Status   string            `json:"status"`
	Ports    []Port            `json:"ports"`
	Labels   map[string]string `json:"labels,omitempty"`
	Networks []string          `json:"networks,omitempty"`
	Mounts   []Mount           `json:"mounts,omitempty"`
	Project  string            `json:"project,omitempty"`
	Service  string            `json:"service,omitempty"`
}

type ContainerDetail struct {
	Container
	Env           []string        `json:"env,omitempty"`
	Cmd           []string        `json:"cmd,omitempty"`
	Entrypoint    []string        `json:"entrypoint,omitempty"`
	WorkingDir    string          `json:"workingDir,omitempty"`
	User          string          `json:"user,omitempty"`
	Hostname      string          `json:"hostname,omitempty"`
	RestartPolicy string          `json:"restartPolicy,omitempty"`
	Tty           bool            `json:"tty"`
	IPAddress     string          `json:"ipAddress,omitempty"`
	CreatedAt     string          `json:"createdAt,omitempty"`
	StartedAt     string          `json:"startedAt,omitempty"`
	FinishedAt    string          `json:"finishedAt,omitempty"`
	RestartCount  int             `json:"restartCount"`
	Platform      string          `json:"platform,omitempty"`
	Raw           json.RawMessage `json:"raw,omitempty"`
}

type ContainerStats struct {
	CPUPercent float64   `json:"cpuPercent"`
	MemUsage   uint64    `json:"memUsage"`
	MemLimit   uint64    `json:"memLimit"`
	MemPercent float64   `json:"memPercent"`
	NetRx      uint64    `json:"netRx"`
	NetTx      uint64    `json:"netTx"`
	BlockRead  uint64    `json:"blockRead"`
	BlockWrite uint64    `json:"blockWrite"`
	Pids       uint64    `json:"pids"`
	Timestamp  time.Time `json:"timestamp"`
}

type CreateContainerRequest struct {
	Name           string            `json:"name"`
	Image          string            `json:"image"`
	Command        string            `json:"command"`
	Entrypoint     string            `json:"entrypoint"`
	Env            []string          `json:"env"`
	Ports          []string          `json:"ports"`
	Volumes        []string          `json:"volumes"`
	Networks       []string          `json:"networks"`
	NetworkAliases []string          `json:"networkAliases"`
	WorkingDir     string            `json:"workingDir"`
	User           string            `json:"user"`
	Hostname       string            `json:"hostname"`
	Tty            bool              `json:"tty"`
	AutoRemove     bool              `json:"autoRemove"`
	Privileged     bool              `json:"privileged"`
	RestartPolicy  string            `json:"restartPolicy"`
	MemoryMB       int64             `json:"memoryMB"`
	CPULimit       float64           `json:"cpuLimit"`
	Labels         map[string]string `json:"labels"`
}

type Image struct {
	ID          string   `json:"id"`
	RepoTags    []string `json:"repoTags"`
	RepoDigests []string `json:"repoDigests,omitempty"`
	Created     int64    `json:"created"`
	Size        int64    `json:"size"`
	Containers  int64    `json:"containers"`
}

type Network struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Driver     string   `json:"driver"`
	Scope      string   `json:"scope"`
	Internal   bool     `json:"internal"`
	Attachable bool     `json:"attachable"`
	Containers []string `json:"containers,omitempty"`
	Created    string   `json:"created,omitempty"`
}

type CreateNetworkRequest struct {
	Name     string `json:"name"`
	Driver   string `json:"driver"`
	Internal bool   `json:"internal"`
}

type Volume struct {
	Name       string `json:"name"`
	Driver     string `json:"driver"`
	Mountpoint string `json:"mountpoint"`
	Scope      string `json:"scope"`
	CreatedAt  string `json:"createdAt,omitempty"`
	InUse      bool   `json:"inUse"`
}

type Info struct {
	Kind              string   `json:"kind"`
	Name              string   `json:"name"`
	ServerVersion     string   `json:"serverVersion"`
	APIVersion        string   `json:"apiVersion"`
	OS                string   `json:"os"`
	Arch              string   `json:"arch"`
	Driver            string   `json:"driver"`
	CgroupVersion     string   `json:"cgroupVersion"`
	KernelVersion     string   `json:"kernelVersion"`
	StorageDriver     string   `json:"storageDriver"`
	RootDir           string   `json:"rootDir"`
	Containers        int      `json:"containers"`
	ContainersRunning int      `json:"containersRunning"`
	ContainersPaused  int      `json:"containersPaused"`
	ContainersStopped int      `json:"containersStopped"`
	Images            int      `json:"images"`
	NCPU              int      `json:"ncpu"`
	MemTotal          int64    `json:"memTotal"`
	Warnings          []string `json:"warnings,omitempty"`
}

type Terminal interface {
	Read(p []byte) (int, error)
	Write(p []byte) (int, error)
	Close() error
	Resize(cols, rows uint) error
}

type ComposeService struct {
	Name          string            `json:"name"`
	Image         string            `json:"image"`
	ContainerName string            `json:"containerName,omitempty"`
	Command       string            `json:"command,omitempty"`
	Entrypoint    string            `json:"entrypoint,omitempty"`
	Environment   []string          `json:"environment,omitempty"`
	Ports         []string          `json:"ports,omitempty"`
	Volumes       []string          `json:"volumes,omitempty"`
	Networks      []string          `json:"networks,omitempty"`
	DependsOn     []string          `json:"dependsOn,omitempty"`
	Restart       string            `json:"restart,omitempty"`
	WorkingDir    string            `json:"workingDir,omitempty"`
	User          string            `json:"user,omitempty"`
	Hostname      string            `json:"hostname,omitempty"`
	Privileged    bool              `json:"privileged,omitempty"`
	Tty           bool              `json:"tty,omitempty"`
	StdInOpen     bool              `json:"stdinOpen,omitempty"`
	MemLimitMB    int64             `json:"memLimitMB,omitempty"`
	CPULimit      float64           `json:"cpuLimit,omitempty"`
	Labels        map[string]string `json:"labels,omitempty"`
}

type ComposeFile struct {
	Name     string           `json:"name"`
	Services []ComposeService `json:"services"`
	Networks []string         `json:"networks"`
	Volumes  []string         `json:"volumes"`
}

type ComposeProject struct {
	Name       string `json:"name"`
	File       string `json:"file"`
	Services   int    `json:"services"`
	Containers int    `json:"containers"`
	Running    int    `json:"running"`
	Status     string `json:"status"`
}

type HostStats struct {
	Timestamp    time.Time   `json:"timestamp"`
	CPUPercent   float64     `json:"cpuPercent"`
	CPUPerCore   []float64   `json:"cpuPerCore,omitempty"`
	Load1        float64     `json:"load1"`
	Load5        float64     `json:"load5"`
	Load15       float64     `json:"load15"`
	MemTotal     uint64      `json:"memTotal"`
	MemUsed      uint64      `json:"memUsed"`
	MemFree      uint64      `json:"memFree"`
	MemAvailable uint64      `json:"memAvailable"`
	MemPercent   float64     `json:"memPercent"`
	SwapTotal    uint64      `json:"swapTotal"`
	SwapUsed     uint64      `json:"swapUsed"`
	Uptime       uint64      `json:"uptime"`
	BootTime     uint64      `json:"bootTime"`
	Hostname     string      `json:"hostname"`
	Platform     string      `json:"platform"`
	Kernel       string      `json:"kernel"`
	CPUModel     string      `json:"cpuModel"`
	CPUCores     int         `json:"cpuCores"`
	Disks        []DiskStats `json:"disks"`
	Networks     []NetStats  `json:"networks"`
	NetRxRate    float64     `json:"netRxRate"`
	NetTxRate    float64     `json:"netTxRate"`
	Processes    uint64      `json:"processes"`
	Proc         ProcStats   `json:"proc"`
}

// ProcStats 是宿主机进程榜的快照，口径与 top 一致：
// CPU 100% = 1 个逻辑核（可超过 100%），内存百分比 = RSS / MemTotal。
type ProcStats struct {
	Total   int        `json:"total"`   // 进程总数
	Running int        `json:"running"` // 处于 R 态的进程数
	Scanned int        `json:"scanned"` // 本轮成功采样的进程数
	TopCPU  []ProcInfo `json:"topCpu"`  // 按 CPU 降序的前 N 个
	TopMem  []ProcInfo `json:"topMem"`  // 按常驻内存降序的前 N 个
}

// ProcInfo 是单个进程的采样结果。CPU 为相邻两次采样间的差分，
// 首次采样（无上一帧）为 0；RSS 不含被换出的部分。
type ProcInfo struct {
	PID     int     `json:"pid"`
	Name    string  `json:"name"`
	User    string  `json:"user"`
	State   string  `json:"state"`
	CPU     float64 `json:"cpuPercent"`
	Mem     float64 `json:"memPercent"`
	RSS     uint64  `json:"rssBytes"`
	Threads int     `json:"threads"`
	Cmdline string  `json:"cmdline,omitempty"`
}

type DiskStats struct {
	Device      string  `json:"device"`
	Mountpoint  string  `json:"mountpoint"`
	Fstype      string  `json:"fstype"`
	Total       uint64  `json:"total"`
	Used        uint64  `json:"used"`
	Free        uint64  `json:"free"`
	UsedPercent float64 `json:"usedPercent"`
}

type NetStats struct {
	Name    string  `json:"name"`
	RxBytes uint64  `json:"rxBytes"`
	TxBytes uint64  `json:"txBytes"`
	RxRate  float64 `json:"rxRate"`
	TxRate  float64 `json:"txRate"`
}
