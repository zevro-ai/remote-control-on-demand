package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"gopkg.in/yaml.v3"
)

type goreleaserConfig struct {
	Builds []goreleaserBuild `yaml:"builds"`
}

type goreleaserBuild struct {
	ID   string   `yaml:"id"`
	Tags []string `yaml:"tags"`
}

func TestGoReleaserBuildsRCODWithReleaseTag(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}

	configPath := filepath.Join(filepath.Dir(currentFile), "..", "..", ".goreleaser.yml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("ReadFile(%q): %v", configPath, err)
	}

	var cfg goreleaserConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("yaml.Unmarshal(%q): %v", configPath, err)
	}

	for _, build := range cfg.Builds {
		if build.ID != "rcod" {
			continue
		}

		for _, tag := range build.Tags {
			if tag == "release" {
				return
			}
		}

		t.Fatalf("rcod build must include release tag, got %v", build.Tags)
	}

	t.Fatal("rcod build not found in .goreleaser.yml")
}
