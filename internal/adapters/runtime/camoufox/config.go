package camoufox

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	defaultPythonPath      = "python3"
	defaultStartupTimeout  = 30 * time.Second
	defaultShutdownTimeout = 5 * time.Second
	defaultTaskTimeout     = 2 * time.Minute
	defaultWSPathPrefix    = "browser-session-"
)

type Config struct {
	PythonPath      string
	ArtifactsDir    string
	StartupTimeout  time.Duration
	ShutdownTimeout time.Duration
	TaskTimeout     time.Duration
	Headless        bool
	ServerPort      int
	WSPathPrefix    string
	ExtraEnv        []string
	ProxyRefs       map[string]string
}

func (c Config) normalize() (Config, error) {
	if c.PythonPath == "" {
		c.PythonPath = defaultPythonPath
	}
	if c.StartupTimeout == 0 {
		c.StartupTimeout = defaultStartupTimeout
	}
	if c.ShutdownTimeout == 0 {
		c.ShutdownTimeout = defaultShutdownTimeout
	}
	if c.TaskTimeout == 0 {
		c.TaskTimeout = defaultTaskTimeout
	}
	if c.WSPathPrefix == "" {
		c.WSPathPrefix = defaultWSPathPrefix
	}
	if c.ArtifactsDir == "" {
		c.ArtifactsDir = filepath.Join(os.TempDir(), "browser-automation-artifacts")
	}
	if c.StartupTimeout < 0 {
		return c, errors.New("startup timeout cannot be negative")
	}
	if c.ShutdownTimeout < 0 {
		return c, errors.New("shutdown timeout cannot be negative")
	}
	if c.TaskTimeout < 0 {
		return c, errors.New("task timeout cannot be negative")
	}
	if c.ServerPort < 0 {
		return c, errors.New("server port cannot be negative")
	}
	if len(c.ProxyRefs) > 0 {
		refs := make(map[string]string, len(c.ProxyRefs))
		for key, value := range c.ProxyRefs {
			key = strings.TrimSpace(key)
			value = strings.TrimSpace(value)
			if key == "" {
				return c, errors.New("proxy refs cannot contain an empty key")
			}
			if value == "" {
				return c, errors.New("proxy refs cannot contain an empty value")
			}
			refs[key] = value
		}
		c.ProxyRefs = refs
	}
	return c, nil
}
