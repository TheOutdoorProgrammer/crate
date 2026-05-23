package provider

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"
)

type ProviderConfig struct {
	Name    string
	Binary  string
	Address string
}

func ParseProviderConfig(s string) []ProviderConfig {
	if s == "" {
		return nil
	}
	var configs []ProviderConfig
	for _, entry := range strings.Split(s, ",") {
		parts := strings.SplitN(strings.TrimSpace(entry), ":", 3)
		if len(parts) != 3 {
			continue
		}
		configs = append(configs, ProviderConfig{
			Name:    parts[0],
			Binary:  parts[1],
			Address: parts[2],
		})
	}
	return configs
}

type ProcessManager struct {
	processes []*os.Process
}

func NewProcessManager() *ProcessManager {
	return &ProcessManager{}
}

func (pm *ProcessManager) StartProviders(ctx context.Context, manager *Manager, configs []ProviderConfig) error {
	for _, cfg := range configs {
		if cfg.Binary == "external" {
			if err := manager.RegisterProvider(ctx, cfg.Name, cfg.Address); err != nil {
				slog.Error("failed to connect to external provider", "name", cfg.Name, "address", cfg.Address, "error", err)
			}
			continue
		}

		port := cfg.Address
		cmd := exec.Command(cfg.Binary)
		cmd.Env = append(os.Environ(), "PORT="+port)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		if err := cmd.Start(); err != nil {
			return fmt.Errorf("start provider %s: %w", cfg.Name, err)
		}
		pm.processes = append(pm.processes, cmd.Process)
		slog.Info("started provider process", "name", cfg.Name, "pid", cmd.Process.Pid, "port", port)

		go func(c *exec.Cmd, name string) {
			if err := c.Wait(); err != nil {
				slog.Error("provider process exited", "name", name, "error", err)
			}
		}(cmd, cfg.Name)

		address := "localhost:" + port
		if err := waitForProvider(ctx, manager, cfg.Name, address, 10*time.Second); err != nil {
			slog.Error("provider failed to become ready", "name", cfg.Name, "error", err)
		}
	}
	return nil
}

func waitForProvider(ctx context.Context, manager *Manager, name, address string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if err := manager.RegisterProvider(ctx, name, address); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	return fmt.Errorf("provider %s not ready after %s", name, timeout)
}

func (pm *ProcessManager) StopAll() {
	for _, p := range pm.processes {
		p.Signal(os.Interrupt)
	}
	time.Sleep(2 * time.Second)
	for _, p := range pm.processes {
		p.Kill()
	}
}
