package policy

import (
	"time"
)

type Duration time.Duration

func (d *Duration) UnmarshalText(text []byte) error {
	dur, err := time.ParseDuration(string(text))
	if err != nil {
		return err
	}
	*d = Duration(dur)
	return nil
}

func (d Duration) Duration() time.Duration {
	return time.Duration(d)
}

type Limit struct {
	Algorithm string   `yaml:"algorithm"`
	Requests  int64    `yaml:"requests"`
	Window    Duration `yaml:"window"`
}

type RouteRule struct {
	Route string `yaml:"route"`
	Limit Limit  `yaml:"limit"`
}

type TierRules struct {
	Limits []RouteRule `yaml:"limits"`
}

type Config struct {
	Tiers map[string]TierRules `yaml:"tiers"`
}
