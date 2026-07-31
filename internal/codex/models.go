package codex

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/zevro-ai/remote-control-on-demand/internal/provider"
)

type modelsCache struct {
	Models []cachedModel `json:"models"`
}

type cachedModel struct {
	Slug                     string `json:"slug"`
	DisplayName              string `json:"display_name"`
	Description              string `json:"description"`
	DefaultReasoningLevel    string `json:"default_reasoning_level"`
	SupportedReasoningLevels []struct {
		Effort string `json:"effort"`
	} `json:"supported_reasoning_levels"`
	Visibility string `json:"visibility"`
}

func listModels(defaultModel string) ([]provider.Model, error) {
	home, err := resolveCodexHome()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(filepath.Join(home, "models_cache.json"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("reading Codex models cache: %w", err)
	}

	var cache modelsCache
	if len(data) > 0 {
		if err := json.Unmarshal(data, &cache); err != nil {
			return nil, fmt.Errorf("parsing Codex models cache: %w", err)
		}
	}

	models := make([]provider.Model, 0, len(cache.Models)+1)
	seen := make(map[string]struct{})
	for _, cached := range cache.Models {
		if strings.TrimSpace(cached.Slug) == "" || strings.EqualFold(cached.Visibility, "hide") {
			continue
		}
		if _, ok := seen[cached.Slug]; ok {
			continue
		}
		seen[cached.Slug] = struct{}{}
		levels := make([]string, 0, len(cached.SupportedReasoningLevels))
		for _, level := range cached.SupportedReasoningLevels {
			if value := strings.TrimSpace(level.Effort); value != "" {
				levels = append(levels, value)
			}
		}
		models = append(models, provider.Model{
			Slug:             cached.Slug,
			DisplayName:      firstNonEmpty(cached.DisplayName, cached.Slug),
			Description:      strings.TrimSpace(cached.Description),
			ReasoningLevels:  uniqueStrings(levels),
			DefaultReasoning: strings.TrimSpace(cached.DefaultReasoningLevel),
		})
	}

	defaultModel = strings.TrimSpace(defaultModel)
	if defaultModel != "" {
		if _, ok := seen[defaultModel]; !ok {
			models = append(models, provider.Model{Slug: defaultModel, DisplayName: defaultModel})
		}
	}

	sort.SliceStable(models, func(i, j int) bool {
		if models[i].Slug == defaultModel {
			return true
		}
		if models[j].Slug == defaultModel {
			return false
		}
		return strings.ToLower(models[i].DisplayName) < strings.ToLower(models[j].DisplayName)
	})
	return models, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
