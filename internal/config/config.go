package config

import (
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server struct {
		Listen          string        `yaml:"listen"`
		ShutdownTimeout time.Duration `yaml:"shutdown_timeout"`
	} `yaml:"server"`

	IPInfo struct {
		DumpURL         string        `yaml:"dump_url"`
		Token           string        `yaml:"token"`
		RefreshInterval time.Duration `yaml:"refresh_interval"`
		DownloadTimeout time.Duration `yaml:"download_timeout"`
		CacheFile       string        `yaml:"cache_file"`
	} `yaml:"ipinfo"`
}

func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return nil, err
	}

	if v := os.Getenv("APP_IPINFO_TOKEN"); v != "" {
		cfg.IPInfo.Token = v
	}

	return &cfg, nil
}
