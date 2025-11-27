package packager

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestZipBuilderAppliesOverlay(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	artifactPath := filepath.Join(tmp, "agent.zip")
	createTestZip(t, artifactPath)
	builder := NewZipBuilder()
	input := BuildInput{
		TenantID:     "tenant",
		Component:    "agent",
		Version:      "1.2.3",
		Platform:     "windows",
		Arch:         "amd64",
		Format:       "zip",
		ArtifactPath: artifactPath,
		OutputDir:    filepath.Join(tmp, "out"),
		OverlayFiles: []OverlayFile{{Path: "config/bootstrap.toml", Data: []byte("token=abc"), Mode: 0o600}},
		ConfigHash:   "abcdef123456",
	}
	result, err := builder.Build(context.Background(), input)
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}
	files := readZipContents(t, result.BundlePath)
	content, ok := files["config/bootstrap.toml"]
	if !ok {
		t.Fatalf("overlay file missing from bundle")
	}
	if string(content) != "token=abc" {
		t.Fatalf("unexpected overlay content: %s", content)
	}
}

func TestZipBuilderWrapsExecutableArtifact(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	artifactPath := filepath.Join(tmp, "printmaster-agent.exe")
	if err := os.WriteFile(artifactPath, []byte("binary"), 0o755); err != nil {
		t.Fatalf("failed to create exe: %v", err)
	}
	builder := NewZipBuilder()
	input := BuildInput{
		TenantID:     "tenant",
		Component:    "agent",
		Version:      "1.2.3",
		Platform:     "windows",
		Arch:         "amd64",
		Format:       "zip",
		ArtifactPath: artifactPath,
		OutputDir:    filepath.Join(tmp, "out"),
		OverlayFiles: []OverlayFile{{Path: "config/bootstrap.toml", Data: []byte("token=wrap"), Mode: 0o600}},
		ConfigHash:   "123456abcdef",
	}
	result, err := builder.Build(context.Background(), input)
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}
	files := readZipContents(t, result.BundlePath)
	if got := string(files["printmaster-agent.exe"]); got != "binary" {
		t.Fatalf("exe contents mismatch: %s", got)
	}
	if got := string(files["config/bootstrap.toml"]); got != "token=wrap" {
		t.Fatalf("overlay missing or invalid: %s", got)
	}
}

func TestTarGzBuilderAppliesOverlay(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	artifactPath := filepath.Join(tmp, "agent.tar.gz")
	createTestTarGz(t, artifactPath)
	builder := NewTarGzBuilder()
	input := BuildInput{
		TenantID:     "tenant",
		Component:    "agent",
		Version:      "1.2.3",
		Platform:     "linux",
		Arch:         "amd64",
		Format:       "tar.gz",
		ArtifactPath: artifactPath,
		OutputDir:    filepath.Join(tmp, "out"),
		OverlayFiles: []OverlayFile{{Path: "config/bootstrap.toml", Data: []byte("token=xyz"), Mode: 0o600}},
		ConfigHash:   "fedcba654321",
	}
	result, err := builder.Build(context.Background(), input)
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}
	files := readTarGzContents(t, result.BundlePath)
	content, ok := files["config/bootstrap.toml"]
	if !ok {
		t.Fatalf("overlay file missing from tar bundle")
	}
	if string(content) != "token=xyz" {
		t.Fatalf("unexpected overlay content: %s", content)
	}
}

func createTestZip(t *testing.T, dest string) {
	t.Helper()
	f, err := os.Create(dest)
	if err != nil {
		t.Fatalf("failed to create zip: %v", err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	files := map[string]string{
		"bin/agent.exe":      "binary",
		"config/default.txt": "defaults",
	}
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("failed to create zip entry: %v", err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("failed to write zip entry: %v", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("failed to close zip: %v", err)
	}
}

func createTestTarGz(t *testing.T, dest string) {
	t.Helper()
	f, err := os.Create(dest)
	if err != nil {
		t.Fatalf("failed to create tar.gz: %v", err)
	}
	defer f.Close()
	gz := gzip.NewWriter(f)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	files := map[string]string{
		"bin/agent":   "binary",
		"config/base": "defaults",
	}
	for name, content := range files {
		hdr := &tar.Header{Name: name, Mode: 0o644, Size: int64(len(content))}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("failed to write tar header: %v", err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatalf("failed to write tar content: %v", err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("failed to close tar: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("failed to close gzip: %v", err)
	}
}

func readZipContents(t *testing.T, path string) map[string][]byte {
	t.Helper()
	r, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("failed to open zip: %v", err)
	}
	defer r.Close()
	out := make(map[string][]byte)
	for _, f := range r.File {
		if f.FileInfo().IsDir() {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("failed to open zip entry: %v", err)
		}
		data, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatalf("failed to read zip entry: %v", err)
		}
		out[f.Name] = data
	}
	return out
}

func readTarGzContents(t *testing.T, path string) map[string][]byte {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("failed to open tar.gz: %v", err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("failed to create gzip reader: %v", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	out := make(map[string][]byte)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("failed to read tar header: %v", err)
		}
		if hdr.FileInfo().IsDir() {
			continue
		}
		var buf bytes.Buffer
		if _, err := io.Copy(&buf, tr); err != nil {
			t.Fatalf("failed to read tar content: %v", err)
		}
		out[hdr.Name] = buf.Bytes()
	}
	return out
}
