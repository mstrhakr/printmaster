package autoupdate

import (
	"context"

	"printmaster/agent/agent"
)

// ClientAdapter adapts agent.ServerClient to the UpdateClient interface.
type ClientAdapter struct {
	client *agent.ServerClient
}

// NewClientAdapter creates a new adapter wrapping the given server client.
func NewClientAdapter(client *agent.ServerClient) *ClientAdapter {
	return &ClientAdapter{client: client}
}

// GetLatestManifest fetches the latest manifest from the server and converts it.
func (a *ClientAdapter) GetLatestManifest(ctx context.Context, component, platform, arch, channel string) (*UpdateManifest, error) {
	manifest, err := a.client.GetLatestManifest(ctx, component, platform, arch, channel)
	if err != nil {
		return nil, err
	}
	return convertManifest(manifest), nil
}

// DownloadArtifact downloads the artifact via the server client.
func (a *ClientAdapter) DownloadArtifact(ctx context.Context, manifest *UpdateManifest, destPath string, resumeFrom int64) (int64, error) {
	return a.DownloadArtifactWithProgress(ctx, manifest, destPath, resumeFrom, nil)
}

// DownloadArtifactWithProgress downloads the artifact with progress reporting.
func (a *ClientAdapter) DownloadArtifactWithProgress(ctx context.Context, manifest *UpdateManifest, destPath string, resumeFrom int64, progressCb DownloadProgressCallback) (int64, error) {
	// Convert back to agent manifest for the download call
	agentManifest := &agent.UpdateManifest{
		ManifestVersion: manifest.ManifestVersion,
		Component:       manifest.Component,
		Version:         manifest.Version,
		MinorLine:       manifest.MinorLine,
		Platform:        manifest.Platform,
		Arch:            manifest.Arch,
		Channel:         manifest.Channel,
		SHA256:          manifest.SHA256,
		SizeBytes:       manifest.SizeBytes,
		SourceURL:       manifest.SourceURL,
		DownloadURL:     manifest.DownloadURL,
		PublishedAt:     manifest.PublishedAt,
		GeneratedAt:     manifest.GeneratedAt,
		Signature:       manifest.Signature,
	}

	// Convert progress callback if provided
	var agentProgressCb agent.DownloadProgressCallback
	if progressCb != nil {
		agentProgressCb = func(percent int, bytesRead int64) {
			progressCb(percent, bytesRead)
		}
	}

	return a.client.DownloadArtifactWithProgress(ctx, agentManifest, destPath, resumeFrom, agentProgressCb)
}

// convertManifest converts from agent.UpdateManifest to autoupdate.UpdateManifest.
func convertManifest(m *agent.UpdateManifest) *UpdateManifest {
	if m == nil {
		return nil
	}
	return &UpdateManifest{
		ManifestVersion: m.ManifestVersion,
		Component:       m.Component,
		Version:         m.Version,
		MinorLine:       m.MinorLine,
		Platform:        m.Platform,
		Arch:            m.Arch,
		Channel:         m.Channel,
		SHA256:          m.SHA256,
		SizeBytes:       m.SizeBytes,
		SourceURL:       m.SourceURL,
		DownloadURL:     m.DownloadURL,
		PublishedAt:     m.PublishedAt,
		GeneratedAt:     m.GeneratedAt,
		Signature:       m.Signature,
	}
}
