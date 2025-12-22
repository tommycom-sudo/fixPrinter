package printer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	defaultEntryURL         = "http://172.20.38.62:8080/webroot/decision/view/report?viewlet=hi%252Fhis%252Fbil%252Ftest_printer.cpt&ref_t=design&op=view&ref_c=093def84-95a2-4eaf-af61-61dc90a4d043"
	defaultPrintURL         = "http://172.20.38.62:8080/webroot/decision/view/report"
	defaultWaitTimeout      = 45 * time.Second
	defaultReadyInterval    = 400 * time.Millisecond
	defaultFrameLoadTimeout = 25 * time.Second
)

// PrintParams represents the payload FineReport expects in FR.doURLPrint.
type PrintParams struct {
	PrintURL    string    `json:"printUrl"`
	PrintType   int       `json:"printType"`
	PageType    int       `json:"pageType"`
	IsPopUp     bool      `json:"isPopUp"`
	PrinterName string    `json:"printerName"`
	Data        PrintData `json:"data"`
	EntryURL    string    `json:"entryUrl,omitempty"`
}

// PrintData wraps reportlets used by FR.doURLPrint.
type PrintData struct {
	Reportlets []Reportlet `json:"reportlets"`
}

// Reportlet defines a single report instance to print.
type Reportlet struct {
	Reportlet      string `json:"reportlet"`
	IdMedpers      string `json:"idMedpers"`
	OrgNa          string `json:"orgNa"`
	IdVismed       string `json:"idVismed"`
	DocumentNumber string `json:"documentNumber"`
}

// PrintResult is used for synchronising async print execution results.
type PrintResult struct {
	RequestID  string `json:"requestId"`
	Success    bool   `json:"success"`
	Error      string `json:"error,omitempty"`
	DurationMS int64  `json:"durationMs,omitempty"`
}

// Config captures service level settings.
type Config struct {
	EntryURL         string
	PrintURL         string
	ReadyTimeout     time.Duration
	ReadyInterval    time.Duration
	FrameLoadTimeout time.Duration
	ResultTimeout    time.Duration
}

// DefaultParams returns the suggested initial print payload.
func DefaultParams() PrintParams {
	return PrintParams{
		PrintURL:    defaultPrintURL,
		PrintType:   1,
		PageType:    0,
		IsPopUp:     false,
		PrinterName: "HP LaserJet Pro P1100 plus series",
		Data: PrintData{
			Reportlets: []Reportlet{
				{
					Reportlet:      "hi/his/bil/test_printer.cpt",
					IdMedpers:      "672315903281201152",
					OrgNa:          "南方医科大学口腔医院",
					IdVismed:       "763843129987043328",
					DocumentNumber: "20251218000001",
				},
			},
		},
		EntryURL: defaultEntryURL,
	}
}

// Service coordinates JS execution with Go callers.
type Service struct {
	cfg Config

	ctx     context.Context
	waiters map[string]chan PrintResult
	mu      sync.Mutex
}

// NewService builds a printer service with sane defaults.
func NewService(cfg Config) *Service {
	if cfg.EntryURL == "" {
		cfg.EntryURL = defaultEntryURL
	}
	if cfg.PrintURL == "" {
		cfg.PrintURL = defaultPrintURL
	}
	if cfg.ReadyTimeout == 0 {
		cfg.ReadyTimeout = defaultWaitTimeout
	}
	if cfg.ReadyInterval == 0 {
		cfg.ReadyInterval = defaultReadyInterval
	}
	if cfg.FrameLoadTimeout == 0 {
		cfg.FrameLoadTimeout = defaultFrameLoadTimeout
	}
	if cfg.ResultTimeout == 0 {
		cfg.ResultTimeout = cfg.ReadyTimeout + 15*time.Second
	}

	return &Service{
		cfg:     cfg,
		waiters: make(map[string]chan PrintResult),
	}
}

// SetContext initialises the runtime context used to invoke JS.
func (s *Service) SetContext(ctx context.Context) {
	s.ctx = ctx
}

// SetEndpoints overrides entry & print URL (useful when routing through a local proxy).
func (s *Service) SetEndpoints(entryURL, printURL string) {
	if entryURL != "" {
		s.cfg.EntryURL = entryURL
	}
	if printURL != "" {
		s.cfg.PrintURL = printURL
	}
}

// EntryURL returns the active entry URL.
func (s *Service) EntryURL() string {
	return s.cfg.EntryURL
}

// PrintURL returns the active print URL.
func (s *Service) PrintURL() string {
	return s.cfg.PrintURL
}

// Print triggers the FR.doURLPrint workflow via injected frontend JS.
func (s *Service) Print(params PrintParams) (*PrintResult, error) {
	if s.ctx == nil {
		return nil, errors.New("runtime context is not ready yet")
	}
	if err := params.validate(); err != nil {
		return nil, err
	}

	requestID := uuid.NewString()
	payload, err := s.preparePayload(requestID, params)
	if err != nil {
		return nil, err
	}

	ch := make(chan PrintResult, 1)
	s.track(requestID, ch)

	script := fmt.Sprintf("window.__xAutoPrint && window.__xAutoPrint.start(%s);", payload)
	runtime.WindowExecJS(s.ctx, script)

	select {
	case result := <-ch:
		if result.Success {
			return &result, nil
		}
		if result.Error == "" {
			result.Error = "unknown printing error"
		}
		return &result, errors.New(result.Error)
	case <-time.After(s.cfg.ResultTimeout):
		s.untrack(requestID)
		return nil, fmt.Errorf("print workflow timed out after %s", s.cfg.ResultTimeout)
	}
}

// NotifyResult is called by the frontend once executePrint completes (success or failure).
func (s *Service) NotifyResult(result PrintResult) {
	if result.RequestID == "" {
		return
	}
	if result.DurationMS == 0 {
		result.DurationMS = s.cfg.ReadyInterval.Milliseconds()
	}

	s.mu.Lock()
	ch, ok := s.waiters[result.RequestID]
	if ok {
		delete(s.waiters, result.RequestID)
	}
	s.mu.Unlock()

	if ok {
		ch <- result
	}
}

func (s *Service) preparePayload(requestID string, params PrintParams) (string, error) {
	entryURL := params.EntryURL
	if entryURL == "" {
		entryURL = s.cfg.EntryURL
	}
	printURL := params.PrintURL
	if printURL == "" {
		printURL = s.cfg.PrintURL
	}
	params.EntryURL = entryURL
	params.PrintURL = printURL
	extended := struct {
		PrintParams
		RequestID          string `json:"requestId"`
		EntryURL           string `json:"entryUrl"`
		ReadyTimeoutMS     int64  `json:"readyTimeoutMs"`
		ReadyIntervalMS    int64  `json:"readyIntervalMs"`
		FrameLoadTimeoutMS int64  `json:"frameLoadTimeoutMs"`
	}{
		PrintParams:        params,
		RequestID:          requestID,
		EntryURL:           entryURL,
		ReadyTimeoutMS:     s.cfg.ReadyTimeout.Milliseconds(),
		ReadyIntervalMS:    s.cfg.ReadyInterval.Milliseconds(),
		FrameLoadTimeoutMS: s.cfg.FrameLoadTimeout.Milliseconds(),
	}

	raw, err := json.Marshal(extended)
	if err != nil {
		return "", fmt.Errorf("serialise print payload: %w", err)
	}
	return string(raw), nil
}

func (s *Service) track(key string, ch chan PrintResult) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.waiters[key] = ch
}

func (s *Service) untrack(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.waiters, key)
}

func (p PrintParams) validate() error {
	if p.PrintURL == "" {
		return errors.New("printUrl is required")
	}
	if p.PrinterName == "" {
		return errors.New("printerName is required")
	}
	if len(p.Data.Reportlets) == 0 {
		return errors.New("at least one reportlet is required")
	}
	return nil
}
