package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/pluto/wecom-dock/internal/registry"
	"gopkg.in/yaml.v3"
)

// Config is the process configuration. Environment variables override the file.
type Config struct {
	Interval       time.Duration
	HealthTimeout  time.Duration
	PullTimeout    time.Duration
	IncludeStopped bool
	OnlyLabeled    bool
	DataDir        string
	DockerHost     string
	DockerConfig   string
	BotID          string
	Secret         string
	WSURL          string
	Admins         []string
	Registries     []registry.Auth
}

type fileConfig struct {
	Interval       string `yaml:"interval"`
	HealthTimeout  string `yaml:"health_timeout"`
	PullTimeout    string `yaml:"pull_timeout"`
	IncludeStopped bool   `yaml:"include_stopped"`
	OnlyLabeled    bool   `yaml:"only_labeled"`
	DataDir        string `yaml:"data_dir"`
	DockerHost     string `yaml:"docker_host"`
	DockerConfig   string `yaml:"docker_config"`
	WeCom          struct {
		BotID  string   `yaml:"bot_id"`
		Secret string   `yaml:"secret"`
		WSURL  string   `yaml:"ws_url"`
		Admins []string `yaml:"admins"`
	} `yaml:"wecom"`
	Registries []struct {
		Host     string `yaml:"host"`
		Username string `yaml:"username"`
		Password string `yaml:"password"`
	} `yaml:"registries"`
}

// Load reads CONFIG or ./config.yaml when that file exists, then applies environment variables.
func Load() (Config, error) {
	cfg := Config{
		Interval:      30 * time.Minute,
		HealthTimeout: 60 * time.Second,
		PullTimeout:   10 * time.Minute,
		DataDir:       "data",
		DockerHost:    "unix:///var/run/docker.sock",
		WSURL:         "wss://openws.work.weixin.qq.com",
	}
	path := os.Getenv("CONFIG")
	if path == "" {
		if _, err := os.Stat("config.yaml"); err == nil {
			path = "config.yaml"
		}
	}
	if path != "" {
		if err := applyFile(&cfg, path); err != nil {
			return Config{}, err
		}
	}
	if err := loadDotEnv(); err != nil {
		return Config{}, err
	}
	if err := applyEnv(&cfg); err != nil {
		return Config{}, err
	}
	if cfg.Interval < time.Minute {
		cfg.Interval = time.Minute
	}
	if cfg.BotID == "" || cfg.Secret == "" {
		return Config{}, fmt.Errorf("需要 WECOM_BOT_ID 和 WECOM_SECRET")
	}
	return cfg, nil
}

func applyFile(cfg *Config, path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var file fileConfig
	if err := yaml.Unmarshal(raw, &file); err != nil {
		return fmt.Errorf("解析 %s: %w", path, err)
	}
	if err := setDuration(&cfg.Interval, file.Interval, path); err != nil {
		return err
	}
	if err := setDuration(&cfg.HealthTimeout, file.HealthTimeout, path); err != nil {
		return err
	}
	if err := setDuration(&cfg.PullTimeout, file.PullTimeout, path); err != nil {
		return err
	}
	cfg.IncludeStopped = file.IncludeStopped
	cfg.OnlyLabeled = file.OnlyLabeled
	if file.DataDir != "" {
		cfg.DataDir = file.DataDir
	}
	if file.DockerHost != "" {
		cfg.DockerHost = file.DockerHost
	}
	if file.DockerConfig != "" {
		cfg.DockerConfig = file.DockerConfig
	}
	if file.WeCom.BotID != "" {
		cfg.BotID = file.WeCom.BotID
	}
	if file.WeCom.Secret != "" {
		cfg.Secret = file.WeCom.Secret
	}
	if file.WeCom.WSURL != "" {
		cfg.WSURL = file.WeCom.WSURL
	}
	if len(file.WeCom.Admins) > 0 {
		cfg.Admins = compact(file.WeCom.Admins)
	}
	for _, item := range file.Registries {
		cfg.Registries = append(cfg.Registries, registry.Auth{
			Host: item.Host, Username: item.Username, Password: item.Password,
		})
	}
	return nil
}

func applyEnv(cfg *Config) error {
	if v := os.Getenv("CHECK_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return fmt.Errorf("CHECK_INTERVAL: %w", err)
		}
		cfg.Interval = d
	}
	if v := os.Getenv("HEALTH_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return fmt.Errorf("HEALTH_TIMEOUT: %w", err)
		}
		cfg.HealthTimeout = d
	}
	if v := os.Getenv("PULL_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return fmt.Errorf("PULL_TIMEOUT: %w", err)
		}
		cfg.PullTimeout = d
	}
	if v, ok := os.LookupEnv("INCLUDE_STOPPED"); ok {
		cfg.IncludeStopped = truthy(v)
	}
	if v, ok := os.LookupEnv("ONLY_LABELED"); ok {
		cfg.OnlyLabeled = truthy(v)
	}
	if v := os.Getenv("DATA_DIR"); v != "" {
		cfg.DataDir = v
	}
	if v := os.Getenv("DOCKER_HOST"); v != "" {
		cfg.DockerHost = v
	}
	if v := os.Getenv("DOCKER_CONFIG"); v != "" {
		cfg.DockerConfig = v
	}
	if v := os.Getenv("WECOM_BOT_ID"); v != "" {
		cfg.BotID = v
	}
	if v := os.Getenv("WECOM_SECRET"); v != "" {
		cfg.Secret = v
	}
	if v := os.Getenv("WECOM_WS_URL"); v != "" {
		cfg.WSURL = v
	}
	if v, ok := os.LookupEnv("WECOM_ADMINS"); ok {
		cfg.Admins = compact(strings.Split(v, ","))
	}
	return nil
}

// loadDotEnv fills unset environment variables from ./.env.
// A value already present in the process environment wins.
func loadDotEnv() error {
	raw, err := os.ReadFile(".env")
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = unquote(strings.TrimSpace(val))
		if key == "" || val == "" || os.Getenv(key) != "" {
			continue
		}
		if err := os.Setenv(key, val); err != nil {
			return err
		}
	}
	return nil
}

func unquote(v string) string {
	if len(v) >= 2 {
		if (v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'') {
			return v[1 : len(v)-1]
		}
	}
	return v
}

func setDuration(dest *time.Duration, raw, path string) error {
	if raw == "" {
		return nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	*dest = d
	return nil
}

func compact(items []string) []string {
	var out []string
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}

func truthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
