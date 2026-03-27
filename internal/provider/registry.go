package provider

import (
	"fmt"
	"sort"
	"sync"
)

type Tool struct {
	Metadata Metadata
	Chat     ChatProvider
	Runtime  RuntimeProvider
}

type Registry struct {
	mu    sync.RWMutex
	tools map[string]*Tool
}

func NewRegistry() *Registry {
	return &Registry{
		tools: make(map[string]*Tool),
	}
}

func (r *Registry) RegisterChat(provider ChatProvider) error {
	if provider == nil {
		return fmt.Errorf("chat provider is required")
	}

	metadata := provider.Metadata()
	if err := validateMetadata(metadata); err != nil {
		return err
	}
	if metadata.Chat == nil {
		return fmt.Errorf("chat capabilities are required for %q", metadata.ID)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	tool, err := r.ensureToolLocked(metadata)
	if err != nil {
		return err
	}
	if tool.Chat != nil {
		return fmt.Errorf("chat provider %q already registered", metadata.ID)
	}
	tool.Chat = provider
	return nil
}

func (r *Registry) RegisterRuntime(provider RuntimeProvider) error {
	if provider == nil {
		return fmt.Errorf("runtime provider is required")
	}

	metadata := provider.Metadata()
	if err := validateMetadata(metadata); err != nil {
		return err
	}
	if metadata.Runtime == nil {
		return fmt.Errorf("runtime capabilities are required for %q", metadata.ID)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	tool, err := r.ensureToolLocked(metadata)
	if err != nil {
		return err
	}
	if tool.Runtime != nil {
		return fmt.Errorf("runtime provider %q already registered", metadata.ID)
	}
	tool.Runtime = provider
	return nil
}

func (r *Registry) ChatProvider(id string) (ChatProvider, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	tool, ok := r.tools[id]
	if !ok || tool.Chat == nil {
		return nil, false
	}
	return tool.Chat, true
}

func (r *Registry) RuntimeProvider(id string) (RuntimeProvider, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	tool, ok := r.tools[id]
	if !ok || tool.Runtime == nil {
		return nil, false
	}
	return tool.Runtime, true
}

func (r *Registry) ChatProviders() []ChatProvider {
	r.mu.RLock()
	defer r.mu.RUnlock()

	ids := r.sortedIDsLocked()
	providers := make([]ChatProvider, 0, len(ids))
	for _, id := range ids {
		if tool := r.tools[id]; tool != nil && tool.Chat != nil {
			providers = append(providers, tool.Chat)
		}
	}
	return providers
}

func (r *Registry) RuntimeProviders() []RuntimeProvider {
	r.mu.RLock()
	defer r.mu.RUnlock()

	ids := r.sortedIDsLocked()
	providers := make([]RuntimeProvider, 0, len(ids))
	for _, id := range ids {
		if tool := r.tools[id]; tool != nil && tool.Runtime != nil {
			providers = append(providers, tool.Runtime)
		}
	}
	return providers
}

func (r *Registry) Tools() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	ids := r.sortedIDsLocked()
	tools := make([]Tool, 0, len(ids))
	for _, id := range ids {
		tool := r.tools[id]
		if tool == nil {
			continue
		}
		tools = append(tools, Tool{
			Metadata: cloneMetadata(tool.Metadata),
			Chat:     tool.Chat,
			Runtime:  tool.Runtime,
		})
	}
	return tools
}

func (r *Registry) ensureToolLocked(metadata Metadata) (*Tool, error) {
	tool, ok := r.tools[metadata.ID]
	if !ok {
		tool = &Tool{Metadata: cloneMetadata(metadata)}
		r.tools[metadata.ID] = tool
		return tool, nil
	}

	merged, err := mergeMetadata(tool.Metadata, metadata)
	if err != nil {
		return nil, err
	}
	tool.Metadata = merged
	return tool, nil
}

func mergeMetadata(existing, incoming Metadata) (Metadata, error) {
	if existing.ID != incoming.ID {
		return Metadata{}, fmt.Errorf("provider ID mismatch: %q vs %q", existing.ID, incoming.ID)
	}
	if existing.DisplayName != "" && incoming.DisplayName != "" && existing.DisplayName != incoming.DisplayName {
		return Metadata{}, fmt.Errorf("provider %q display name mismatch: %q vs %q", existing.ID, existing.DisplayName, incoming.DisplayName)
	}

	merged := cloneMetadata(existing)
	if merged.DisplayName == "" {
		merged.DisplayName = incoming.DisplayName
	}
	if incoming.Chat != nil {
		capabilities := *incoming.Chat
		merged.Chat = &capabilities
	}
	if incoming.Runtime != nil {
		capabilities := *incoming.Runtime
		merged.Runtime = &capabilities
	}
	return merged, nil
}

func (r *Registry) sortedIDsLocked() []string {
	ids := make([]string, 0, len(r.tools))
	for id := range r.tools {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
