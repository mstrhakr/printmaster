package autoupdate

import (
	"context"

	"printmaster/agent/agent"
)

// TelemetryAdapter adapts agent.ServerClient to the TelemetrySink interface.
type TelemetryAdapter struct {
	client *agent.ServerClient
}

// NewTelemetryAdapter creates a new telemetry adapter wrapping the given server client.
func NewTelemetryAdapter(client *agent.ServerClient) *TelemetryAdapter {
	return &TelemetryAdapter{client: client}
}

// ReportUpdateStatus sends update telemetry to the server.
func (a *TelemetryAdapter) ReportUpdateStatus(ctx context.Context, payload TelemetryPayload) error {
	return a.client.ReportUpdateStatus(
		ctx,
		string(payload.Status),
		payload.RunID,
		payload.CurrentVersion,
		payload.TargetVersion,
		payload.ErrorCode,
		payload.ErrorMessage,
		payload.Metadata,
	)
}
