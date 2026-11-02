package dockerx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
)

// Container is one Docker container the monitor can see.
type Container struct {
	ID      string
	Name    string
	Image   string
	ImageID string
	State   string
	OS      string
	Arch    string
	Variant string
	Labels  map[string]string
	Digests []string
}

// Client talks to the Docker engine through its socket.
type Client struct {
	api         *client.Client
	pullTimeout time.Duration
	locks       sync.Map
}

// New does not require the engine to be up yet.
func New(host string, pullTimeout time.Duration) (*Client, error) {
	opts := []client.Opt{client.WithAPIVersionNegotiation()}
	if host != "" {
		opts = append(opts, client.WithHost(host))
	} else {
		opts = append(opts, client.FromEnv)
	}
	api, err := client.NewClientWithOpts(opts...)
	if err != nil {
		return nil, err
	}
	if pullTimeout <= 0 {
		pullTimeout = 10 * time.Minute
	}
	return &Client{api: api, pullTimeout: pullTimeout}, nil
}

// Close releases the HTTP client used by the engine SDK.
func (c *Client) Close() error { return c.api.Close() }

// Ping reports whether the engine accepts requests.
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.api.Ping(ctx)
	return err
}

// List returns containers. all includes stopped ones.
func (c *Client) List(ctx context.Context, all bool) ([]Container, error) {
	items, err := c.api.ContainerList(ctx, container.ListOptions{All: all})
	if err != nil {
		return nil, err
	}
	out := make([]Container, 0, len(items))
	for _, item := range items {
		name := ""
		if len(item.Names) > 0 {
			name = strings.TrimPrefix(item.Names[0], "/")
		}
		ct := Container{
			ID:      item.ID,
			Name:    name,
			Image:   item.Image,
			ImageID: item.ImageID,
			State:   item.State,
			Labels:  item.Labels,
		}
		if item.ImageID != "" {
			ins, _, ierr := c.api.ImageInspectWithRaw(ctx, item.ImageID)
			if ierr == nil {
				ct.Digests = ins.RepoDigests
				ct.OS = ins.Os
				ct.Arch = ins.Architecture
				ct.Variant = ins.Variant
			}
		}
		out = append(out, ct)
	}
	return out, nil
}

// Upgrade pulls the container's tag and recreates it when the image changed.
func (c *Client) Upgrade(ctx context.Context, id, auth string, health time.Duration) (string, error) {
	mu := c.mutex(id)
	mu.Lock()
	defer mu.Unlock()
	eng := &dockerEngine{api: c.api, auth: auth, pullTimeout: c.pullTimeout, health: health}
	return runUpdate(ctx, eng, id, backupName(id))
}

func (c *Client) mutex(id string) *sync.Mutex {
	v, _ := c.locks.LoadOrStore(id, &sync.Mutex{})
	return v.(*sync.Mutex)
}

type dockerEngine struct {
	api         *client.Client
	auth        string
	pullTimeout time.Duration
	health      time.Duration
	cached      types.ContainerJSON
}

func (e *dockerEngine) inspect(ctx context.Context, id string) (snapshot, error) {
	ins, err := e.api.ContainerInspect(ctx, id)
	if err != nil {
		return snapshot{}, err
	}
	e.cached = ins
	ref := ""
	if ins.Config != nil {
		ref = ins.Config.Image
	}
	return snapshot{ID: ins.ID, Name: ins.Name, ImageRef: ref, ImageID: ins.Image}, nil
}

func (e *dockerEngine) pull(ctx context.Context, ref string) error {
	pctx, cancel := context.WithTimeout(ctx, e.pullTimeout)
	defer cancel()
	rc, err := e.api.ImagePull(pctx, ref, image.PullOptions{RegistryAuth: e.auth})
	if err != nil {
		return err
	}
	defer rc.Close()
	dec := json.NewDecoder(rc)
	for {
		var msg struct {
			Error string `json:"error"`
		}
		if err := dec.Decode(&msg); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if msg.Error != "" {
			return errors.New(msg.Error)
		}
	}
}

func (e *dockerEngine) imageID(ctx context.Context, ref string) (string, error) {
	ins, _, err := e.api.ImageInspectWithRaw(ctx, ref)
	if err != nil {
		return "", err
	}
	return ins.ID, nil
}

func (e *dockerEngine) rename(ctx context.Context, id, name string) error {
	return e.api.ContainerRename(ctx, id, name)
}

func (e *dockerEngine) stop(ctx context.Context, id string) error {
	sec := 15
	return e.api.ContainerStop(ctx, id, container.StopOptions{Timeout: &sec})
}

func (e *dockerEngine) createFrom(ctx context.Context, oldID, name string) (string, error) {
	if e.cached.ID != oldID {
		ins, err := e.api.ContainerInspect(ctx, oldID)
		if err != nil {
			return "", err
		}
		e.cached = ins
	}
	return createClone(ctx, e.api, e.cached, name)
}

func (e *dockerEngine) start(ctx context.Context, id string) error {
	err := e.api.ContainerStart(ctx, id, container.StartOptions{})
	if err == nil {
		return nil
	}
	if strings.Contains(strings.ToLower(err.Error()), "already") {
		return nil
	}
	return err
}

func (e *dockerEngine) remove(ctx context.Context, id string) error {
	return e.api.ContainerRemove(ctx, id, container.RemoveOptions{Force: true})
}

func (e *dockerEngine) waitReady(ctx context.Context, id string) error {
	timeout := e.health
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	deadline := time.Now().Add(timeout)
	var stable time.Time
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		ins, err := e.api.ContainerInspect(ctx, id)
		if err != nil {
			return err
		}
		if ins.State == nil {
			return fmt.Errorf("容器没有状态")
		}
		if !ins.State.Running && ins.State.Status != "created" && ins.State.Status != "restarting" {
			return fmt.Errorf("容器已退出，状态 %s，退出码 %d", ins.State.Status, ins.State.ExitCode)
		}
		if ins.State.Health != nil {
			switch ins.State.Health.Status {
			case "healthy":
				return nil
			case "unhealthy":
				return fmt.Errorf("健康检查未通过")
			}
		} else if ins.State.Running {
			if stable.IsZero() {
				stable = time.Now()
			}
			if time.Since(stable) >= 3*time.Second {
				return nil
			}
		}
		if time.Now().After(deadline) {
			if ins.State.Running {
				return nil
			}
			return fmt.Errorf("等待容器就绪超时")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

func createClone(ctx context.Context, api *client.Client, ins types.ContainerJSON, name string) (string, error) {
	if ins.Config == nil {
		return "", fmt.Errorf("容器没有配置")
	}
	cfg := *ins.Config
	// Docker fills an empty hostname with the container id. Keep a hostname the user set.
	shortID := ins.ID
	if len(shortID) > 12 {
		shortID = shortID[:12]
	}
	if cfg.Hostname == ins.ID || cfg.Hostname == shortID {
		cfg.Hostname = ""
	}
	var host container.HostConfig
	if ins.HostConfig != nil {
		host = *ins.HostConfig
		if len(host.Mounts) > 0 {
			host.Binds = nil
		}
	}
	created, err := api.ContainerCreate(ctx, &cfg, &host, networkingConfig(ins), nil, name)
	if err != nil {
		return "", err
	}
	return created.ID, nil
}

func networkingConfig(ins types.ContainerJSON) *network.NetworkingConfig {
	if ins.HostConfig != nil {
		mode := string(ins.HostConfig.NetworkMode)
		if mode == "host" || mode == "none" || strings.HasPrefix(mode, "container:") {
			return nil
		}
	}
	if ins.NetworkSettings == nil || len(ins.NetworkSettings.Networks) == 0 {
		return nil
	}
	eps := map[string]*network.EndpointSettings{}
	for name, ep := range ins.NetworkSettings.Networks {
		if ep == nil {
			continue
		}
		clone := &network.EndpointSettings{
			Links:      ep.Links,
			Aliases:    ep.Aliases,
			DriverOpts: ep.DriverOpts,
		}
		if ep.IPAMConfig != nil {
			clone.IPAMConfig = ep.IPAMConfig
		}
		if ep.MacAddress != "" {
			clone.MacAddress = ep.MacAddress
		}
		eps[name] = clone
	}
	if len(eps) == 0 {
		return nil
	}
	return &network.NetworkingConfig{EndpointsConfig: eps}
}
