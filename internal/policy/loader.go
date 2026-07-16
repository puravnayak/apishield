package policy

import (
	"fmt"
	"io"
	"os"

	"gopkg.in/yaml.v3"
)

func LoadConfig(path string) (*Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open policy config file: %w", err)
	}
	defer file.Close()

	return LoadConfigFromReader(file)
}

func LoadConfigFromReader(r io.Reader) (*Config, error) {
	var cfg Config
	decoder := yaml.NewDecoder(r)
	if err := decoder.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("failed to decode policy config: %w", err)
	}
	return &cfg, nil
}

func (c *Config) GetLimit(tier string, route string) (Limit, bool) {
	tierRules, ok := c.Tiers[tier]
	if !ok {
		return Limit{}, false
	}

	for _, rule := range tierRules.Limits {
		if rule.Route == route {
			return rule.Limit, true
		}
	}

	for _, rule := range tierRules.Limits {
		if rule.Route == "/*" || rule.Route == "*" {
			return rule.Limit, true
		}
	}

	return Limit{}, false
}
