package compose

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"dockerview/internal/model"
)

type rawFile struct {
	Name     string                `yaml:"name"`
	Services map[string]rawService `yaml:"services"`
	Networks map[string]any        `yaml:"networks"`
	Volumes  map[string]any        `yaml:"volumes"`
}

type rawService struct {
	Image         string     `yaml:"image"`
	Build         any        `yaml:"build"`
	ContainerName string     `yaml:"container_name"`
	Command       any        `yaml:"command"`
	Entrypoint    any        `yaml:"entrypoint"`
	Environment   any        `yaml:"environment"`
	EnvFile       any        `yaml:"env_file"`
	Ports         []string   `yaml:"ports"`
	Volumes       []string   `yaml:"volumes"`
	Networks      any        `yaml:"networks"`
	DependsOn     any        `yaml:"depends_on"`
	Restart       string     `yaml:"restart"`
	WorkingDir    string     `yaml:"working_dir"`
	User          string     `yaml:"user"`
	Hostname      string     `yaml:"hostname"`
	Privileged    bool       `yaml:"privileged"`
	Tty           bool       `yaml:"tty"`
	StdinOpen     bool       `yaml:"stdin_open"`
	MemLimit      int64      `yaml:"mem_limit"`
	CPUs          float64    `yaml:"cpus"`
	Deploy        *rawDeploy `yaml:"deploy"`
	Labels        any        `yaml:"labels"`
}

type rawDeploy struct {
	Resources struct {
		Limits struct {
			CPUs   string `yaml:"cpus"`
			Memory string `yaml:"memory"`
		} `yaml:"limits"`
	} `yaml:"resources"`
}

func Parse(content []byte) (model.ComposeFile, error) {
	var raw rawFile
	if err := yaml.Unmarshal(content, &raw); err != nil {
		return model.ComposeFile{}, fmt.Errorf("invalid compose YAML: %w", err)
	}
	if len(raw.Services) == 0 {
		return model.ComposeFile{}, errors.New("compose file has no services")
	}
	result := model.ComposeFile{
		Name: raw.Name,
	}
	names := make([]string, 0, len(raw.Services))
	for name := range raw.Services {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		svc := raw.Services[name]
		if strings.TrimSpace(svc.Image) == "" && svc.Build == nil {
			return model.ComposeFile{}, fmt.Errorf("service %q must define image or build", name)
		}
		out := model.ComposeService{
			Name:          name,
			Image:         svc.Image,
			ContainerName: svc.ContainerName,
			Command:       stringify(svc.Command),
			Entrypoint:    stringify(svc.Entrypoint),
			Environment:   envList(svc.Environment),
			Ports:         svc.Ports,
			Volumes:       svc.Volumes,
			Networks:      stringList(svc.Networks),
			DependsOn:     stringList(svc.DependsOn),
			Restart:       svc.Restart,
			WorkingDir:    svc.WorkingDir,
			User:          svc.User,
			Hostname:      svc.Hostname,
			Privileged:    svc.Privileged,
			Tty:           svc.Tty,
			StdInOpen:     svc.StdinOpen,
			CPULimit:      svc.CPUs,
			Labels:        labelMap(svc.Labels),
		}
		if svc.MemLimit > 0 {
			out.MemLimitMB = parseMemoryMB(strconv.FormatInt(svc.MemLimit, 10))
		}
		if svc.Deploy != nil {
			limits := svc.Deploy.Resources.Limits
			if limits.CPUs != "" {
				if v, err := strconv.ParseFloat(limits.CPUs, 64); err == nil {
					out.CPULimit = v
				}
			}
			if limits.Memory != "" {
				out.MemLimitMB = parseMemoryMB(limits.Memory)
			}
		}
		result.Services = append(result.Services, out)
	}

	for name := range raw.Networks {
		result.Networks = append(result.Networks, name)
	}
	for name := range raw.Volumes {
		result.Volumes = append(result.Volumes, name)
	}
	sort.Strings(result.Networks)
	sort.Strings(result.Volumes)
	return result, nil
}

func stringify(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case []any:
		parts := make([]string, 0, len(t))
		for _, item := range t {
			parts = append(parts, fmt.Sprint(item))
		}
		return strings.Join(parts, " ")
	case []string:
		return strings.Join(t, " ")
	default:
		return fmt.Sprint(t)
	}
}

func stringList(v any) []string {
	switch t := v.(type) {
	case nil:
		return nil
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if s, ok := item.(string); ok {
				out = append(out, s)
				continue
			}
			if m, ok := item.(map[string]any); ok {
				for key := range m {
					out = append(out, key)
				}
			}
		}
		return out
	case []string:
		return t
	case string:
		return []string{t}
	case map[string]any:
		out := make([]string, 0, len(t))
		for key := range t {
			out = append(out, key)
		}
		sort.Strings(out)
		return out
	default:
		return nil
	}
}

func envList(v any) []string {
	var out []string
	switch t := v.(type) {
	case nil:
		return nil
	case []any:
		for _, item := range t {
			out = append(out, fmt.Sprint(item))
		}
	case []string:
		out = append(out, t...)
	case map[string]any:
		keys := make([]string, 0, len(t))
		for key := range t {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			val := t[key]
			if val == nil {
				out = append(out, key)
				continue
			}
			out = append(out, key+"="+fmt.Sprint(val))
		}
	}
	return out
}

func labelMap(v any) map[string]string {
	out := map[string]string{}
	switch t := v.(type) {
	case map[string]any:
		for key, val := range t {
			out[key] = fmt.Sprint(val)
		}
	case []any:
		for _, item := range t {
			s := fmt.Sprint(item)
			parts := strings.SplitN(s, "=", 2)
			if len(parts) == 2 {
				out[parts[0]] = parts[1]
			}
		}
	}
	return out
}

func parseMemoryMB(s string) int64 {
	s = strings.TrimSpace(strings.ToLower(s))
	multiplier := int64(1)
	switch {
	case strings.HasSuffix(s, "gb") || strings.HasSuffix(s, "g"):
		multiplier = 1024
		s = strings.TrimSuffix(strings.TrimSuffix(s, "gb"), "g")
	case strings.HasSuffix(s, "mb") || strings.HasSuffix(s, "m"):
		multiplier = 1
		s = strings.TrimSuffix(strings.TrimSuffix(s, "mb"), "m")
	case strings.HasSuffix(s, "kb") || strings.HasSuffix(s, "k"):
		s = strings.TrimSuffix(strings.TrimSuffix(s, "kb"), "k")
		v, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return 0
		}
		return v / 1024
	default:
		v, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return 0
		}
		return v / (1024 * 1024)
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0
	}
	return v * multiplier
}
