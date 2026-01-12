package monitor

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

// Scheduler manages and executes monitoring tasks on a schedule
type Scheduler struct {
	cron       *cron.Cron
	executor   *Executor
	config     *Config
	configPath string
	mu         sync.RWMutex
	ctx        context.Context
	cancel     context.CancelFunc
}

// NewScheduler creates a new Scheduler
func NewScheduler(config *Config, configPath string) *Scheduler {
	ctx, cancel := context.WithCancel(context.Background())

	return &Scheduler{
		cron: cron.New(cron.WithSeconds()),
		executor: NewExecutor(),
		config:   config,
		configPath: configPath,
		ctx:      ctx,
		cancel:   cancel,
	}
}

// Start starts the scheduler
func (s *Scheduler) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Stop existing cron if any
	if s.cron != nil {
		s.cron.Stop()
	}

	// Create new cron instance
	s.cron = cron.New(cron.WithSeconds())

	// Load tasks from config
	tasks := s.config.GetTasks()
	for _, task := range tasks {
		if task.Enabled && task.Cron != "" {
			if err := s.addTask(task); err != nil {
				log.Printf("[ERROR] Failed to schedule task '%s': %v", task.Name, err)
			} else {
				log.Printf("[INFO] Scheduled task '%s' with cron: %s", task.Name, task.Cron)
			}
		}
	}

	s.cron.Start()
	log.Printf("[INFO] Scheduler started with %d tasks", len(s.cron.Entries()))
	return nil
}

// Stop stops the scheduler
func (s *Scheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cron != nil {
		s.cron.Stop()
	}
	s.cancel()
	log.Printf("[INFO] Scheduler stopped")
}

// Reload reloads the configuration and restarts the scheduler
func (s *Scheduler) Reload() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Reload config
	newConfig, err := LoadConfig(s.configPath)
	if err != nil {
		return fmt.Errorf("reload config failed: %w", err)
	}

	s.config = newConfig
	return s.Restart()
}

// Restart restarts the scheduler with current config
func (s *Scheduler) Restart() error {
	return s.Start()
}

// AddTask adds a new task to the scheduler
func (s *Scheduler) AddTask(task TaskConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Add to config
	s.config.AddTask(task)

	// Save config
	if err := s.config.SaveConfig(s.configPath); err != nil {
		return fmt.Errorf("save config failed: %w", err)
	}

	// Schedule if enabled
	if task.Enabled {
		return s.addTask(task)
	}

	return nil
}

// RemoveTask removes a task from the scheduler
func (s *Scheduler) RemoveTask(taskName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.config.RemoveTask(taskName)

	if err := s.config.SaveConfig(s.configPath); err != nil {
		return fmt.Errorf("save config failed: %w", err)
	}

	return s.Restart()
}

// UpdateTask updates an existing task
func (s *Scheduler) UpdateTask(task TaskConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Remove old task
	s.config.RemoveTask(task.Name)

	// Add updated task
	s.config.AddTask(task)

	if err := s.config.SaveConfig(s.configPath); err != nil {
		return fmt.Errorf("save config failed: %w", err)
	}

	return s.Restart()
}

// GetStatus returns the current status of all tasks
func (s *Scheduler) GetStatus() map[string]TaskStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()

	status := make(map[string]TaskStatus)
	tasks := s.config.GetTasks()

	for _, task := range tasks {
		status[task.Name] = TaskStatus{
			Name:         task.Name,
			Cron:         task.Cron,
			Enabled:      task.Enabled,
			LastExecuted: task.LastExecuted,
			LastStatus:   task.LastStatus,
			LastError:    task.LastError,
		}
	}

	return status
}

// addTask adds a single task to the cron scheduler
func (s *Scheduler) addTask(task TaskConfig) error {
	if task.Cron == "" {
		return fmt.Errorf("cron expression is empty")
	}

	// Parse the curl command
	parsed, err := ParseCURLCommand(task.CURL)
	if err != nil {
		return fmt.Errorf("parse curl failed: %w", err)
	}

	// Create job function
	jobFunc := func() {
		s.executeTask(task.Name, parsed, task.TimeoutMs)
	}

	// Add to cron
	_, err = s.cron.AddFunc(task.Cron, jobFunc)
	return err
}

// executeTask executes a single monitoring task
func (s *Scheduler) executeTask(taskName string, parsed *ParsedRequest, timeoutMs int64) {
	startTime := time.Now()
	log.Printf("[INFO] Executing task '%s'", taskName)

	// Execute the request
	result := s.executor.Execute(s.ctx, parsed, timeoutMs)

	// Update status
	status := "success"
	if result.TimedOut || !result.Success || result.ErrorMessage != "" {
		status = "failed"
	}

	s.config.UpdateTaskStatus(taskName, status, result.ErrorMessage)

	// Check if we need to send an alert
	if result.TimedOut || result.ErrorMessage != "" || !result.Success {
		// Send alert via pushplus
		if s.config.PushPlusToken != "" {
			if err := s.executor.SendAlert(s.config.PushPlusToken, taskName, result, timeoutMs); err != nil {
				log.Printf("[ERROR] Failed to send alert: %v", err)
			}
		}
	} else if result.DurationMs > timeoutMs {
		// Slow but successful request
		if s.config.PushPlusToken != "" {
			if err := s.executor.SendAlert(s.config.PushPlusToken, taskName, result, timeoutMs); err != nil {
				log.Printf("[ERROR] Failed to send alert: %v", err)
			}
		}
	}

	duration := time.Since(startTime)

	// Log detailed result
	if result.ResponseBody != "" {
		// Limit response body in log to avoid huge logs
		maxLogLen := 500
		responseLog := result.ResponseBody
		if len(responseLog) > maxLogLen {
			responseLog = responseLog[:maxLogLen] + "... (truncated)"
		}
		log.Printf("[INFO] Task '%s' completed - Status: %s, HTTP %d, Duration: %dms, Response: %s",
			taskName, status, result.StatusCode, duration.Milliseconds(), responseLog)
	} else {
		log.Printf("[INFO] Task '%s' completed - Status: %s, Duration: %dms, Error: %s",
			taskName, status, duration.Milliseconds(), result.ErrorMessage)
	}

	// Auto-save config to persist status
	_ = s.config.SaveConfig(s.configPath)
}

// TaskStatus represents the status of a task
type TaskStatus struct {
	Name         string `json:"name"`
	Cron         string `json:"cron"`
	Enabled      bool   `json:"enabled"`
	LastExecuted string `json:"lastExecuted"`
	LastStatus   string `json:"lastStatus"`
	LastError    string `json:"lastError"`
}
