package engine

import (
	"context"
	"io"

	"dockerview/internal/model"
)

type Engine interface {
	Kind() string
	Info(ctx context.Context) (model.Info, error)

	ListContainers(ctx context.Context, all bool) ([]model.Container, error)
	InspectContainer(ctx context.Context, id string) (model.ContainerDetail, error)
	CreateContainer(ctx context.Context, req model.CreateContainerRequest) (string, error)
	StartContainer(ctx context.Context, id string) error
	StopContainer(ctx context.Context, id string) error
	RestartContainer(ctx context.Context, id string) error
	PauseContainer(ctx context.Context, id string) error
	UnpauseContainer(ctx context.Context, id string) error
	KillContainer(ctx context.Context, id string, signal string) error
	RemoveContainer(ctx context.Context, id string, force, volumes bool) error
	ContainerStats(ctx context.Context, id string) (model.ContainerStats, error)
	ContainerLogs(ctx context.Context, id string, tail int, timestamps bool) (io.ReadCloser, bool, error)
	ContainerTerminal(ctx context.Context, id string, cols, rows uint) (model.Terminal, func(), error)

	ListImages(ctx context.Context) ([]model.Image, error)
	PullImage(ctx context.Context, ref string, progress io.Writer) error
	RemoveImage(ctx context.Context, id string, force bool) error

	ListNetworks(ctx context.Context) ([]model.Network, error)
	CreateNetwork(ctx context.Context, req model.CreateNetworkRequest) (string, error)
	RemoveNetwork(ctx context.Context, id string) error

	ListVolumes(ctx context.Context) ([]model.Volume, error)
	CreateVolume(ctx context.Context, name, driver string) (string, error)
	RemoveVolume(ctx context.Context, name string, force bool) error

	ComposeUp(ctx context.Context, file model.ComposeFile) error
	ComposeDown(ctx context.Context, project string) error
	ComposeRestart(ctx context.Context, project string) error
	ComposeStop(ctx context.Context, project string) error
	ComposeStart(ctx context.Context, project string) error
	ComposeProjects(ctx context.Context) ([]model.ComposeProject, error)

	Close() error
}
