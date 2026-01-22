package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strings"
	"time"

	"printmaster/common/report"
)

// ReportSubmitter handles building and submitting device data reports.
type ReportSubmitter struct {
	agentVersion string
	anonymizer   *report.Anonymizer
	logger       interface {
		Info(msg string, args ...interface{})
		Warn(msg string, args ...interface{})
		Error(msg string, args ...interface{})
	}
}

// NewReportSubmitter creates a new report submitter.
func NewReportSubmitter(agentVersion string, salt string, logger interface {
	Info(msg string, args ...interface{})
	Warn(msg string, args ...interface{})
	Error(msg string, args ...interface{})
}) *ReportSubmitter {
	return &ReportSubmitter{
		agentVersion: agentVersion,
		anonymizer:   report.NewAnonymizer(salt),
		logger:       logger,
	}
}

// BuildReport creates a diagnostic report from device data and parse debug info.
func (s *ReportSubmitter) BuildReport(
	issueType report.IssueType,
	expectedValue string,
	userMessage string,
	deviceIP string,
	deviceSerial string,
	deviceModel string,
	deviceMAC string,
	currentManufacturer string,
	currentHostname string,
	currentPageCount int,
	parseDebug *ParseDebug,
	recentLogs []string,
) *report.DiagnosticReport {

	r := &report.DiagnosticReport{
		ReportID:  generateReportID(),
		Timestamp: time.Now(),

		AgentVersion: s.agentVersion,
		OS:           runtime.GOOS,
		Arch:         runtime.GOARCH,

		IssueType:     issueType,
		ExpectedValue: expectedValue,
		UserMessage:   userMessage,

		// Anonymize as needed
		DeviceIP:     s.anonymizer.AnonymizeIP(deviceIP),
		DeviceSerial: deviceSerial, // Keep serial for device identification
		DeviceModel:  deviceModel,
		DeviceMAC:    deviceMAC,

		CurrentManufacturer: currentManufacturer,
		CurrentModel:        deviceModel,
		CurrentSerial:       deviceSerial,
		CurrentHostname:     s.anonymizer.AnonymizeHostname(currentHostname),
		CurrentPageCount:    currentPageCount,
	}

	// Add parse debug data if available
	if parseDebug != nil {
		r.DetectedVendor = parseDebug.FinalManufacturer
		r.DetectionSteps = parseDebug.Steps

		// Convert raw PDUs
		for _, pdu := range parseDebug.RawPDUs {
			r.SNMPResponses = append(r.SNMPResponses, report.SNMPResponse{
				OID:      pdu.OID,
				Type:     pdu.Type,
				Value:    pdu.StrValue,
				HexValue: pdu.HexValue,
			})
		}
	}

	// Sanitize and add logs
	for _, logEntry := range recentLogs {
		r.RecentLogs = append(r.RecentLogs, report.SanitizeLogEntry(logEntry, s.anonymizer))
	}

	return r
}

// SubmitToProxy sends the report to the proxy service and returns the response.
func (s *ReportSubmitter) SubmitToProxy(ctx context.Context, r *report.DiagnosticReport) (*report.ProxyResponse, error) {
	payload := report.ProxyRequest{Report: *r}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal report: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, report.ProxyEndpoint, bytes.NewReader(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", fmt.Sprintf("PrintMaster-Agent/%s", s.agentVersion))

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("proxy request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1MB limit
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("proxy returned status %d: %s", resp.StatusCode, string(body))
	}

	var proxyResp report.ProxyResponse
	if err := json.Unmarshal(body, &proxyResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &proxyResp, nil
}

// generateReportID creates a unique report identifier.
func generateReportID() string {
	timestamp := time.Now().UnixNano() / 1e6 // milliseconds
	// Simple encoding: base36 timestamp + random suffix
	return fmt.Sprintf("RPT-%X-%X", timestamp, time.Now().UnixNano()%0xFFFF)
}

// ReportRequest represents the incoming request from the web UI.
type ReportRequest struct {
	ReportID      string `json:"report_id,omitempty"`
	IssueType     string `json:"issue_type"`
	ExpectedValue string `json:"expected_value"`
	UserMessage   string `json:"user_message"`

	DeviceIP     string `json:"device_ip"`
	DeviceSerial string `json:"device_serial"`
	DeviceModel  string `json:"device_model"`
	DeviceMAC    string `json:"device_mac"`

	CurrentManufacturer string `json:"current_manufacturer"`
	CurrentModel        string `json:"current_model"`
	CurrentSerial       string `json:"current_serial"`
	CurrentHostname     string `json:"current_hostname"`
	CurrentPageCount    int    `json:"current_page_count"`

	DetectedVendor   string                `json:"detected_vendor"`
	DetectionSteps   []string              `json:"detection_steps"`
	SNMPResponses    []report.SNMPResponse `json:"snmp_responses"`
}

// ParseIssueType converts a string to an IssueType.
func ParseIssueType(s string) report.IssueType {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "wrong_manufacturer":
		return report.IssueWrongManufacturer
	case "wrong_model":
		return report.IssueWrongModel
	case "missing_serial":
		return report.IssueMissingSerial
	case "wrong_serial":
		return report.IssueWrongSerial
	case "incorrect_counters":
		return report.IssueIncorrectCounters
	case "missing_toner":
		return report.IssueMissingToner
	case "missing_supplies":
		return report.IssueMissingSupplies
	case "wrong_hostname":
		return report.IssueWrongHostname
	default:
		return report.IssueOther
	}
}

// ReportDeviceData sends collected device data to the server (legacy stub).
func ReportDeviceData(data interface{}) error {
	// Deprecated: Use ReportSubmitter instead
	return nil
}
