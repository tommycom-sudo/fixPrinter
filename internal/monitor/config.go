package monitor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

const (
	defaultConfigFile = "monitor.json"
	defaultPushPlusURL = "https://www.pushplus.plus/send"
)

// Config represents the monitoring configuration
type Config struct {
	PushPlusToken string       `json:"pushPlusToken"`
	Tasks         []TaskConfig `json:"tasks"`
	mu            sync.RWMutex `json:"-"`
}

// TaskConfig represents a single monitoring task
type TaskConfig struct {
	Name         string            `json:"name"`
	Cron         string            `json:"cron"`
	CURL         string            `json:"curl"`
	TimeoutMs    int64             `json:"timeoutMs"`
	Enabled      bool              `json:"enabled"`
	LastExecuted string            `json:"lastExecuted,omitempty"`
	LastStatus   string            `json:"lastStatus,omitempty"`
	LastError    string            `json:"lastError,omitempty"`
}

// ParsedRequest represents a parsed curl command
type ParsedRequest struct {
	URL     string            `json:"url"`
	Method  string            `json:"method"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
}

// LoadConfig loads the configuration from file
func LoadConfig(configPath string) (*Config, error) {
	if configPath == "" {
		configPath = defaultConfigFile
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Return default config if file doesn't exist
			return &Config{
				PushPlusToken: "",
				Tasks:         []TaskConfig{},
			}, nil
		}
		return nil, err
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// SaveConfig saves the configuration to file
func (c *Config) SaveConfig(configPath string) error {
	if configPath == "" {
		configPath = defaultConfigFile
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Ensure directory exists
	dir := filepath.Dir(configPath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(configPath, data, 0644)
}

// AddTask adds a new monitoring task
func (c *Config) AddTask(task TaskConfig) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Tasks = append(c.Tasks, task)
}

// RemoveTask removes a task by name
func (c *Config) RemoveTask(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, t := range c.Tasks {
		if t.Name == name {
			c.Tasks = append(c.Tasks[:i], c.Tasks[i+1:]...)
			break
		}
	}
}

// GetTask returns a task by name
func (c *Config) GetTask(name string) *TaskConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for i := range c.Tasks {
		if c.Tasks[i].Name == name {
			return &c.Tasks[i]
		}
	}
	return nil
}

// GetTasks returns all tasks
func (c *Config) GetTasks() []TaskConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.Tasks
}

// UpdateTaskStatus updates the execution status of a task
func (c *Config) UpdateTaskStatus(name, status, errorMsg string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := range c.Tasks {
		if c.Tasks[i].Name == name {
			c.Tasks[i].LastStatus = status
			if errorMsg != "" {
				c.Tasks[i].LastError = errorMsg
			}
			break
		}
	}
}
