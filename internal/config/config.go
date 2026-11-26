// Package config reads leo.yaml, which holds per-repo overrides.
package config

import (
	"bytes"
	"fmt"
	"os"

	"go.yaml.in/yaml/v3"
)

type Config struct {
	Repos map[string]Repo `yaml:"repos"`
}

type Repo struct {
	// Unshipped lists paths that don't reach users, on top of .github/ and, in pug-sdk repos,
	// lockfiles.
	Unshipped []string `yaml:"unshipped"`
	// Codegen sets toolchain versions for a proto sync's codegen job, where the SDK's CI differs
	// from sync.yml's defaults.
	Codegen Codegen `yaml:"codegen"`
	// SkipProtos leaves the SDK out of proto syncs, for while a branch of its own carries one.
	SkipProtos bool `yaml:"skip_protos"`
}

type Codegen struct {
	Bun    string `yaml:"bun"`
	Node   string `yaml:"node"`
	Protoc string `yaml:"protoc"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(data)
}

func Parse(data []byte) (*Config, error) {
	var cfg Config
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("leo.yaml: %w", err)
	}
	return &cfg, nil
}

func (c *Config) Repo(name string) Repo {
	if c == nil {
		return Repo{}
	}
	return c.Repos[name]
}
