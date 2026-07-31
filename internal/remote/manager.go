package remote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/zevro-ai/remote-control-on-demand/internal/provider"
)

type HostManager struct {
	mu        sync.Mutex
	statePath string
	hosts     map[string]HostConfig
	clients   map[string]*AgentClient
	providers map[string][]string
}

func (m *HostManager) Close() {
	m.mu.Lock()
	clients := make([]*AgentClient, 0, len(m.clients))
	for _, client := range m.clients {
		clients = append(clients, client)
	}
	m.clients = make(map[string]*AgentClient)
	m.mu.Unlock()
	for _, client := range clients {
		_ = client.Close()
	}
}

func NewHostManager(initial []HostConfig, statePath string) (*HostManager, error) {
	m := &HostManager{
		statePath: statePath,
		hosts:     make(map[string]HostConfig),
		clients:   make(map[string]*AgentClient),
		providers: make(map[string][]string),
	}
	for _, host := range initial {
		if err := m.put(host); err != nil {
			return nil, err
		}
	}
	if statePath != "" {
		data, err := os.ReadFile(statePath)
		if err == nil {
			var saved []HostConfig
			if err := json.Unmarshal(data, &saved); err != nil {
				return nil, fmt.Errorf("parsing SSH host state: %w", err)
			}
			for _, host := range saved {
				if err := m.put(host); err != nil {
					return nil, fmt.Errorf("loading SSH host %q: %w", host.ID, err)
				}
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("reading SSH host state: %w", err)
		}
	}
	return m, nil
}

func (m *HostManager) put(host HostConfig) error {
	if err := host.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(host.BaseFolder) == "" {
		return fmt.Errorf("SSH host %q requires base_folder", host.ID)
	}
	m.hosts[host.ID] = host
	return nil
}

func (m *HostManager) List() []HostRuntime {
	m.mu.Lock()
	hosts := make([]HostConfig, 0, len(m.hosts))
	for _, host := range m.hosts {
		hosts = append(hosts, host)
	}
	m.mu.Unlock()

	result := make([]HostRuntime, 0, len(hosts))
	for _, host := range hosts {
		runtime := HostRuntime{Identity: host.Identity(), Status: HostStatusUnknown}
		started := time.Now()
		client, err := m.client(host)
		if err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			err = client.Ping(ctx)
			cancel()
		}
		runtime.LastCheckedAt = time.Now()
		runtime.LatencyMs = time.Since(started).Milliseconds()
		if err != nil {
			runtime.Status = HostStatusUnreachable
			runtime.LastError = err.Error()
		} else {
			runtime.Status = HostStatusReachable
		}
		result = append(result, runtime)
	}
	return result
}

func (m *HostManager) Add(host HostConfig, registry *provider.Registry) error {
	m.mu.Lock()
	if _, exists := m.hosts[host.ID]; exists {
		m.mu.Unlock()
		return fmt.Errorf("SSH host %q already exists", host.ID)
	}
	if err := m.put(host); err != nil {
		m.mu.Unlock()
		return err
	}
	m.mu.Unlock()
	if err := m.registerHost(host, registry); err != nil {
		m.mu.Lock()
		delete(m.hosts, host.ID)
		m.mu.Unlock()
		return err
	}
	return m.save()
}

func (m *HostManager) Remove(id string, registry *provider.Registry) error {
	id = strings.TrimSpace(id)
	m.mu.Lock()
	host, exists := m.hosts[id]
	if exists {
		delete(m.hosts, id)
	}
	client := m.clients[id]
	delete(m.clients, id)
	providerIDs := append([]string(nil), m.providers[id]...)
	delete(m.providers, id)
	m.mu.Unlock()
	if !exists {
		return fmt.Errorf("SSH host %q not found", id)
	}
	if client != nil {
		_ = client.Close()
	}
	if registry != nil {
		for _, providerID := range providerIDs {
			registry.UnregisterChat(providerID)
		}
	}
	_ = host
	return m.save()
}

func (m *HostManager) RegisterAll(registry *provider.Registry) error {
	m.mu.Lock()
	hosts := make([]HostConfig, 0, len(m.hosts))
	for _, host := range m.hosts {
		hosts = append(hosts, host)
	}
	m.mu.Unlock()
	for _, host := range hosts {
		if err := m.registerHost(host, registry); err != nil {
			return err
		}
	}
	return nil
}

func (m *HostManager) registerHost(host HostConfig, registry *provider.Registry) error {
	client, err := m.client(host)
	if err != nil {
		return err
	}
	ids := make([]string, 0, 2)
	for _, providerID := range []string{"codex", "antigravity"} {
		proxy, err := NewProxyProvider(host.ID, host.Name, providerID, client)
		if err != nil {
			return err
		}
		if registry != nil {
			if err := registry.RegisterChat(proxy); err != nil {
				return err
			}
		}
		ids = append(ids, proxy.Metadata().ID)
	}
	m.mu.Lock()
	m.providers[host.ID] = ids
	m.mu.Unlock()
	return nil
}

func (m *HostManager) client(host HostConfig) (*AgentClient, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if client := m.clients[host.ID]; client != nil {
		return client, nil
	}
	executor, err := NewExecutor(host)
	if err != nil {
		return nil, err
	}
	command := strings.TrimSpace(host.AgentCommand)
	if command == "" {
		command = "rcod-agent"
	}
	parts := strings.Fields(command)
	hasBaseFolder := false
	for _, part := range parts {
		if part == "--base-folder" {
			hasBaseFolder = true
		}
	}
	if !hasBaseFolder {
		parts = append(parts, "--base-folder", host.BaseFolder)
	}
	client, err := NewAgentClientArgs(executor, host.AgentWorkDir, parts)
	if err != nil {
		return nil, err
	}
	m.clients[host.ID] = client
	return client, nil
}

func (m *HostManager) save() error {
	if m.statePath == "" {
		return nil
	}
	m.mu.Lock()
	hosts := make([]HostConfig, 0, len(m.hosts))
	for _, host := range m.hosts {
		hosts = append(hosts, host)
	}
	m.mu.Unlock()
	data, err := json.MarshalIndent(hosts, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.statePath, append(data, '\n'), 0600)
}
