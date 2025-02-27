package database

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"net"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/docker/errdefs"
	"github.com/docker/docker/pkg/jsonmessage"
	"github.com/docker/go-connections/nat"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	PostgresImage = "postgres:16"
	containerName = "skyvault-db"
)

// StartPostgresContainer starts a PostgreSQL container with persistent storage
// and checks for readiness with a PING using exponential backoff.
func StartPostgresContainer(ctx context.Context, dbURL string) error {
	// Validate URL
	user, pass, host, port, name, err := parseURL(dbURL)
	if err != nil {
		return fmt.Errorf("parsing URL: %w", err)
	}
	slog.DebugContext(ctx, "starting PostgreSQL container", "dbURL", dbURL, "user", user, "pass", pass, "host", host, "port", port, "name", name)

	// If postgres is already running, return
	if checkPostgresReady(ctx, dbURL, 1) == nil {
		return nil
	}

	// Set up Docker client
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return fmt.Errorf("creating Docker client: %w", err)
	}

	// Pull PostgreSQL image if not available
	reader, err := cli.ImagePull(ctx, PostgresImage, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("pulling Docker image: %w", err)
	}
	defer reader.Close()

	// Wait for image pull to complete and display progress
	termFd := os.Stdout.Fd()
	if err := jsonmessage.DisplayJSONMessagesStream(reader, os.Stdout, termFd, true, nil); err != nil {
		return fmt.Errorf("displaying pull progress: %w", err)
	}

	// Create a custom bridge network named "skyvault-bridge" if it doesn't exist
	_, err = cli.NetworkCreate(ctx, "skyvault-bridge", network.CreateOptions{
		Driver: "bridge",
	})
	if err != nil && !errdefs.IsConflict(err) {
		return fmt.Errorf("creating network: %w", err)
	}

	// Define container configurations
	containerConfig := &container.Config{
		Image: PostgresImage,
		Env: []string{
			"POSTGRES_USER=" + user,
			"POSTGRES_PASSWORD=" + pass,
			"POSTGRES_DB=" + name,
		},
		ExposedPorts: nat.PortSet{
			nat.Port("5432/tcp"): struct{}{},
		},
	}

	// Define host configuration with port mapping, volume mount and network isolation
	hostConfig := &container.HostConfig{
		PortBindings: nat.PortMap{
			nat.Port("5432/tcp"): []nat.PortBinding{
				{
					HostIP:   host,
					HostPort: port,
				},
			},
		},
		Mounts: []mount.Mount{
			{
				Type:   mount.TypeVolume,
				Source: "skyvault_data",
				Target: "/var/lib/postgresql/data",
			},
		},
	}

	networkConfig := &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{
			"skyvault-bridge": {},
		},
	}

	// Create and start the PostgreSQL container
	var containerID string
	resp, err := cli.ContainerCreate(ctx, containerConfig, hostConfig, networkConfig, nil, containerName)
	if err != nil {
		if !errdefs.IsConflict(err) {
			return fmt.Errorf("creating container: %w", err)
		}

		resp, err := cli.ContainerInspect(ctx, containerName)
		if err != nil {
			return fmt.Errorf("inspecting container: %w", err)
		}

		containerID = resp.ID
	} else {
		containerID = resp.ID
	}

	if err := cli.ContainerStart(ctx, containerID, container.StartOptions{}); err != nil {
		return fmt.Errorf("starting container: %w", err)
	}

	// Check readiness with exponential backoff
	if err := checkPostgresReady(ctx, dbURL, 30); err != nil {
		return fmt.Errorf("PostgreSQL readiness check: %w", err)
	}

	// Return container ID and stop function
	return nil
}

func parseURL(dbURL string) (string, string, string, string, string, error) {
	u, err := url.Parse(dbURL)
	if err != nil {
		return "", "", "", "", "", fmt.Errorf("parsing URL: %w", err)
	}

	host, port, err := net.SplitHostPort(u.Host)
	if err != nil {
		host = u.Host
		port = ""
	}

	pass, ok := u.User.Password()
	if !ok {
		return "", "", host, port, strings.TrimPrefix(u.Path, "/"), nil
	}

	return u.User.Username(), pass, host, port, strings.TrimPrefix(u.Path, "/"), nil
}

// checkPostgresReady checks if PostgreSQL is ready by pinging it.
func checkPostgresReady(ctx context.Context, dbURL string, attempts int) error {
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		return fmt.Errorf("creating connection pool: %w", err)
	}
	defer pool.Close()

	var backoff time.Duration
	for i := 0; i < attempts; i++ {
		err = pool.Ping(ctx)
		if err == nil {
			return nil
		}

		backoff = time.Duration(math.Pow(2, float64(i))) * 100 * time.Millisecond
		slog.Info("PostgreSQL is not ready, retrying", "backoff", backoff, "error", err)
		time.Sleep(backoff)
	}

	return fmt.Errorf("PostgreSQL is not ready after multiple attempts")
}
