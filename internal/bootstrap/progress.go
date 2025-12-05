package bootstrap

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type ProgressReporter struct {
	apiURL         string
	token          string
	client         *http.Client
	sentSystemInfo bool
}

type ProgressRequest struct {
	Token      string                 `json:"token"`
	Status     string                 `json:"status"`
	Step       string                 `json:"step,omitempty"`
	Message    string                 `json:"message,omitempty"`
	Percent    int                    `json:"percent,omitempty"`
	SystemInfo map[string]interface{} `json:"system_info,omitempty"`
}

func NewProgressReporter(apiURL, token string) *ProgressReporter {
	return &ProgressReporter{
		apiURL: apiURL,
		token:  token,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

func (p *ProgressReporter) Report(status, step, message string, percent int, systemInfo map[string]interface{}) error {
	req := ProgressRequest{
		Token:   p.token,
		Status:  status,
		Step:    step,
		Message: message,
		Percent: percent,
	}

	// Include system_info only on first call
	if !p.sentSystemInfo && systemInfo != nil {
		req.SystemInfo = systemInfo
		p.sentSystemInfo = true
	}

	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to marshal progress request: %w", err)
	}

	httpReq, err := http.NewRequest("POST", p.apiURL+"/api/v1/bootstrap/progress", bytes.NewBuffer(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("API returned non-200 status: %d", resp.StatusCode)
	}

	return nil
}

func (p *ProgressReporter) ReportError(message string) error {
	return p.Report("failed", "", message, 0, nil)
}
