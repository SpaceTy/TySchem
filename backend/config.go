package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

// Config is loaded from a TOML file (default config.toml, override with the
// CONFIG_FILE environment variable). A missing file is not an error: built-in
// defaults are used so the server still starts on a fresh checkout.
type Config struct {
	Port  int         `toml:"port"`
	Admin AdminConfig `toml:"admin"`
}

// AdminConfig provisions the built-in administrator account. The account is
// created at startup if missing, promoted to admin, and its password is kept
// in sync with this value.
type AdminConfig struct {
	Username string `toml:"username"`
	Password string `toml:"password"`
}

func defaultConfig() Config {
	return Config{Port: 8080}
}

// LoadConfig parses path into a Config. A non-existent path yields defaults.
func LoadConfig(path string) (Config, error) {
	cfg := defaultConfig()
	if path == "" {
		return cfg, nil
	}
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return cfg, err
	}
	md, err := toml.DecodeFile(path, &cfg)
	if err != nil {
		return cfg, fmt.Errorf("parse %s: %w", path, err)
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		return cfg, fmt.Errorf("unknown config keys: %v", undecoded)
	}
	if cfg.Port == 0 {
		cfg.Port = 8080
	}
	if cfg.Port < 1 || cfg.Port > 65535 {
		return cfg, fmt.Errorf("port must be between 1 and 65535, got %d", cfg.Port)
	}
	return cfg, nil
}
