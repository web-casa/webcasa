package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/registry"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
)

// Client wraps the Docker Engine API client with convenience methods.
type Client struct {
	cli *client.Client
}

// NewClient creates a Client connected to the Docker daemon.
// socketPath defaults to /var/run/docker.sock if empty.
func NewClient(socketPath string) (*Client, error) {
	if socketPath == "" {
		socketPath = "/var/run/docker.sock"
	}
	cli, err := client.NewClientWithOpts(
		client.WithHost("unix://"+socketPath),
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return nil, err
	}
	return &Client{cli: cli}, nil
}

// Close releases the Docker client resources.
func (c *Client) Close() error {
	return c.cli.Close()
}

// Ping checks if Docker daemon is reachable.
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.cli.Ping(ctx)
	return err
}

// ── Containers ──

// ContainerInfo is a simplified container representation for API responses.
type ContainerInfo struct {
	ID      string            `json:"id"`
	Name    string            `json:"name"`
	Image   string            `json:"image"`
	State   string            `json:"state"`   // running, exited, paused, etc.
	Status  string            `json:"status"`  // human-readable, e.g. "Up 2 hours"
	Created int64             `json:"created"` // unix timestamp
	Ports   []PortBinding     `json:"ports"`
	Labels  map[string]string `json:"labels"`
}

// PortBinding is a simplified port mapping.
type PortBinding struct {
	HostPort      string `json:"host_port"`
	ContainerPort string `json:"container_port"`
	Protocol      string `json:"protocol"`
}

// ListContainers returns all containers (including stopped).
func (c *Client) ListContainers(ctx context.Context, all bool) ([]ContainerInfo, error) {
	containers, err := c.cli.ContainerList(ctx, container.ListOptions{All: all})
	if err != nil {
		return nil, err
	}

	result := make([]ContainerInfo, 0, len(containers))
	for _, ctr := range containers {
		name := ""
		if len(ctr.Names) > 0 {
			name = strings.TrimPrefix(ctr.Names[0], "/")
		}

		ports := make([]PortBinding, 0)
		for _, p := range ctr.Ports {
			ports = append(ports, PortBinding{
				HostPort:      portStr(p.PublicPort),
				ContainerPort: portStr(p.PrivatePort),
				Protocol:      p.Type,
			})
		}

		result = append(result, ContainerInfo{
			ID:      ctr.ID[:12],
			Name:    name,
			Image:   ctr.Image,
			State:   ctr.State,
			Status:  ctr.Status,
			Created: ctr.Created,
			Ports:   ports,
			Labels:  ctr.Labels,
		})
	}
	return result, nil
}

// StartContainer starts a stopped container.
func (c *Client) StartContainer(ctx context.Context, id string) error {
	return c.cli.ContainerStart(ctx, id, container.StartOptions{})
}

// StopContainer stops a running container with a timeout.
func (c *Client) StopContainer(ctx context.Context, id string) error {
	timeout := 10
	return c.cli.ContainerStop(ctx, id, container.StopOptions{Timeout: &timeout})
}

// RestartContainer restarts a container.
func (c *Client) RestartContainer(ctx context.Context, id string) error {
	timeout := 10
	return c.cli.ContainerRestart(ctx, id, container.StopOptions{Timeout: &timeout})
}

// RemoveContainer removes a container (force).
func (c *Client) RemoveContainer(ctx context.Context, id string) error {
	return c.cli.ContainerRemove(ctx, id, container.RemoveOptions{Force: true})
}

// ContainerLogs returns the log output for a container.
func (c *Client) ContainerLogs(ctx context.Context, id string, tail string, follow bool) (io.ReadCloser, error) {
	if tail == "" {
		tail = "200"
	}
	return c.cli.ContainerLogs(ctx, id, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Tail:       tail,
		Follow:     follow,
		Timestamps: true,
	})
}

// ContainerStats returns a single snapshot of container resource usage.
type ContainerStats struct {
	CPUPercent float64 `json:"cpu_percent"`
	MemUsage   uint64  `json:"mem_usage"`
	MemLimit   uint64  `json:"mem_limit"`
	MemPercent float64 `json:"mem_percent"`
	NetRx      uint64  `json:"net_rx"`
	NetTx      uint64  `json:"net_tx"`
}

// GetContainerStats returns a single stats snapshot.
func (c *Client) GetContainerStats(ctx context.Context, id string) (*ContainerStats, error) {
	resp, err := c.cli.ContainerStatsOneShot(ctx, id)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var statsJSON types.StatsJSON
	if err := decodeJSON(resp.Body, &statsJSON); err != nil {
		return nil, err
	}

	cpuDelta := float64(statsJSON.CPUStats.CPUUsage.TotalUsage - statsJSON.PreCPUStats.CPUUsage.TotalUsage)
	sysDelta := float64(statsJSON.CPUStats.SystemUsage - statsJSON.PreCPUStats.SystemUsage)
	cpuPercent := 0.0
	if sysDelta > 0 && cpuDelta > 0 {
		cpuPercent = (cpuDelta / sysDelta) * float64(statsJSON.CPUStats.OnlineCPUs) * 100.0
	}

	memPercent := 0.0
	if statsJSON.MemoryStats.Limit > 0 {
		memPercent = float64(statsJSON.MemoryStats.Usage) / float64(statsJSON.MemoryStats.Limit) * 100.0
	}

	var netRx, netTx uint64
	for _, v := range statsJSON.Networks {
		netRx += v.RxBytes
		netTx += v.TxBytes
	}

	return &ContainerStats{
		CPUPercent: cpuPercent,
		MemUsage:   statsJSON.MemoryStats.Usage,
		MemLimit:   statsJSON.MemoryStats.Limit,
		MemPercent: memPercent,
		NetRx:      netRx,
		NetTx:      netTx,
	}, nil
}

// ── Images ──

// ImageInfo is a simplified image representation.
type ImageInfo struct {
	ID      string   `json:"id"`
	Tags    []string `json:"tags"`
	Size    int64    `json:"size"`
	Created int64    `json:"created"`
}

// ListImages returns all local images.
func (c *Client) ListImages(ctx context.Context) ([]ImageInfo, error) {
	images, err := c.cli.ImageList(ctx, image.ListOptions{All: false})
	if err != nil {
		return nil, err
	}

	result := make([]ImageInfo, 0, len(images))
	for _, img := range images {
		id := img.ID
		if strings.HasPrefix(id, "sha256:") {
			id = id[7:19] // short hash
		}
		result = append(result, ImageInfo{
			ID:      id,
			Tags:    img.RepoTags,
			Size:    img.Size,
			Created: img.Created,
		})
	}
	return result, nil
}

// PullImage pulls an image. Returns a reader for progress output.
func (c *Client) PullImage(ctx context.Context, refStr string) (io.ReadCloser, error) {
	return c.cli.ImagePull(ctx, refStr, image.PullOptions{})
}

// RemoveImage removes an image.
func (c *Client) RemoveImage(ctx context.Context, id string) error {
	_, err := c.cli.ImageRemove(ctx, id, image.RemoveOptions{Force: true, PruneChildren: true})
	return err
}

// ── Networks ──

// NetworkInfo is a simplified network representation.
type NetworkInfo struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Driver     string `json:"driver"`
	Scope      string `json:"scope"`
	Internal   bool   `json:"internal"`
	Containers int    `json:"containers"`
}

// ListNetworks returns all Docker networks.
func (c *Client) ListNetworks(ctx context.Context) ([]NetworkInfo, error) {
	nets, err := c.cli.NetworkList(ctx, network.ListOptions{})
	if err != nil {
		return nil, err
	}

	result := make([]NetworkInfo, 0, len(nets))
	for _, n := range nets {
		result = append(result, NetworkInfo{
			ID:         n.ID[:12],
			Name:       n.Name,
			Driver:     n.Driver,
			Scope:      n.Scope,
			Internal:   n.Internal,
			Containers: len(n.Containers),
		})
	}
	return result, nil
}

// CreateNetwork creates a bridge network.
func (c *Client) CreateNetwork(ctx context.Context, name string) (string, error) {
	resp, err := c.cli.NetworkCreate(ctx, name, network.CreateOptions{
		Driver: "bridge",
	})
	if err != nil {
		return "", err
	}
	return resp.ID[:12], nil
}

// RemoveNetwork removes a network.
func (c *Client) RemoveNetwork(ctx context.Context, id string) error {
	return c.cli.NetworkRemove(ctx, id)
}

// ── Volumes ──

// VolumeInfo is a simplified volume representation.
type VolumeInfo struct {
	Name       string `json:"name"`
	Driver     string `json:"driver"`
	Mountpoint string `json:"mountpoint"`
	CreatedAt  string `json:"created_at"`
}

// ListVolumes returns all Docker volumes.
func (c *Client) ListVolumes(ctx context.Context) ([]VolumeInfo, error) {
	resp, err := c.cli.VolumeList(ctx, volume.ListOptions{})
	if err != nil {
		return nil, err
	}

	result := make([]VolumeInfo, 0, len(resp.Volumes))
	for _, v := range resp.Volumes {
		result = append(result, VolumeInfo{
			Name:       v.Name,
			Driver:     v.Driver,
			Mountpoint: v.Mountpoint,
			CreatedAt:  v.CreatedAt,
		})
	}
	return result, nil
}

// CreateVolume creates a named volume.
func (c *Client) CreateVolume(ctx context.Context, name string) error {
	_, err := c.cli.VolumeCreate(ctx, volume.CreateOptions{Name: name})
	return err
}

// RemoveVolume removes a volume.
func (c *Client) RemoveVolume(ctx context.Context, name string) error {
	return c.cli.VolumeRemove(ctx, name, true)
}

// PruneImages removes unused images.
func (c *Client) PruneImages(ctx context.Context) (uint64, error) {
	report, err := c.cli.ImagesPrune(ctx, filters.Args{})
	if err != nil {
		return 0, err
	}
	return report.SpaceReclaimed, nil
}

// SearchResult represents a Docker Hub search result.
type SearchResult struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	StarCount   int    `json:"star_count"`
	IsOfficial  bool   `json:"is_official"`
}

// SearchImages searches Docker Hub for images.
func (c *Client) SearchImages(ctx context.Context, term string, limit int) ([]SearchResult, error) {
	if limit <= 0 || limit > 25 {
		limit = 25
	}
	results, err := c.cli.ImageSearch(ctx, term, registry.SearchOptions{Limit: limit})
	if err != nil {
		return nil, err
	}
	out := make([]SearchResult, 0, len(results))
	for _, r := range results {
		out = append(out, SearchResult{
			Name:        r.Name,
			Description: r.Description,
			StarCount:   r.StarCount,
			IsOfficial:  r.IsOfficial,
		})
	}
	return out, nil
}

// ── System ──

// SystemInfo returns Docker engine info summary.
type SystemSummary struct {
	ServerVersion string `json:"server_version"`
	Containers    int    `json:"containers"`
	Running       int    `json:"running"`
	Paused        int    `json:"paused"`
	Stopped       int    `json:"stopped"`
	Images        int    `json:"images"`
}

// Info returns system-level Docker information.
func (c *Client) Info(ctx context.Context) (*SystemSummary, error) {
	info, err := c.cli.Info(ctx)
	if err != nil {
		return nil, err
	}
	return &SystemSummary{
		ServerVersion: info.ServerVersion,
		Containers:    info.Containers,
		Running:       info.ContainersRunning,
		Paused:        info.ContainersPaused,
		Stopped:       info.ContainersStopped,
		Images:        info.Images,
	}, nil
}

// ── Helpers ──

func portStr(port uint16) string {
	if port == 0 {
		return ""
	}
	return fmt.Sprintf("%d", port)
}

func defaultTimeout() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 30*time.Second)
}

func decodeJSON(r io.Reader, v interface{}) error {
	return json.NewDecoder(r).Decode(v)
}
