package config

import (
	"errors"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Duration is a YAML duration parsed once at load, never re-parsed by its readers.
type Duration struct {
	text  string
	value time.Duration
}

func (d *Duration) UnmarshalYAML(node *yaml.Node) error {
	return node.Decode(&d.text)
}

// TimeoutOr is the configured timeout, or the fallback when none or zero was set.
func (d Duration) TimeoutOr(fallback time.Duration) time.Duration {
	if d.value > 0 {
		return d.value
	}
	return fallback
}

// TTLOr is the configured lifetime, or the fallback when none was set; zero is a value.
func (d Duration) TTLOr(fallback time.Duration) time.Duration {
	if strings.TrimSpace(d.text) == "" {
		return fallback
	}
	return d.value
}

func (d *Duration) parse() error {
	text := strings.TrimSpace(d.text)
	if text == "" {
		d.value = 0
		return nil
	}
	value, err := time.ParseDuration(text)
	if err != nil {
		return err
	}
	if value < 0 {
		return errors.New("must not be negative")
	}
	d.value = value
	return nil
}
