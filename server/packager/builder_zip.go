package packager

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// ZipBuilder injects overlay files into Zip-based release artifacts.
type ZipBuilder struct{}

// NewZipBuilder returns a builder capable of handling .zip artifacts.
func NewZipBuilder() *ZipBuilder {
	return &ZipBuilder{}
}

// Format reports the packager format identifier handled by this builder.
func (b *ZipBuilder) Format() string { return "zip" }

// Build produces a tenant-scoped archive with overlay files applied.
func (b *ZipBuilder) Build(ctx context.Context, input BuildInput) (*BuildResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(input.OutputDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to ensure output directory: %w", err)
	}
	outPath := filepath.Join(input.OutputDir, buildOutputFilename(input, ".zip"))
	outFile, err := os.Create(outPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create bundle: %w", err)
	}
	defer func() {
		outFile.Close()
		if err != nil {
			os.Remove(outPath)
		}
	}()

	writer := zip.NewWriter(outFile)
	defer writer.Close()

	overlayMap := make(map[string]OverlayFile, len(input.OverlayFiles))
	for _, overlay := range input.OverlayFiles {
		overlayMap[overlay.Path] = overlay
	}

	if err := b.writeArtifactContents(ctx, writer, input.ArtifactPath, overlayMap); err != nil {
		return nil, err
	}

	if err := addOverlayFilesToZip(writer, overlayMap); err != nil {
		return nil, err
	}

	if err := writer.Close(); err != nil {
		return nil, err
	}
	if err := outFile.Close(); err != nil {
		return nil, err
	}

	info, err := os.Stat(outPath)
	if err != nil {
		return nil, err
	}
	meta := map[string]interface{}{
		"builder":         "zip",
		"source_artifact": filepath.Base(input.ArtifactPath),
		"overlay_count":   len(input.OverlayFiles),
	}
	return &BuildResult{BundlePath: outPath, SizeBytes: info.Size(), Metadata: meta}, nil
}

func (b *ZipBuilder) writeArtifactContents(ctx context.Context, writer *zip.Writer, artifactPath string, overlays map[string]OverlayFile) error {
	pathLower := strings.ToLower(artifactPath)
	switch {
	case strings.HasSuffix(pathLower, ".zip"):
		return copyArchiveEntries(ctx, writer, artifactPath, overlays)
	case strings.HasSuffix(pathLower, ".exe"):
		return addStandaloneFile(writer, artifactPath, overlays)
	default:
		return fmt.Errorf("zip builder requires .zip or .exe artifact (got %s)", filepath.Ext(artifactPath))
	}
}

func copyArchiveEntries(ctx context.Context, writer *zip.Writer, artifactPath string, overlays map[string]OverlayFile) error {
	source, err := zip.OpenReader(artifactPath)
	if err != nil {
		return fmt.Errorf("failed to open artifact zip: %w", err)
	}
	defer source.Close()
	for _, entry := range source.File {
		if err := ctx.Err(); err != nil {
			return err
		}
		normalized := normalizeZipEntryPath(entry.Name)
		if normalized == "" {
			continue
		}
		if _, overridden := overlays[normalized]; overridden {
			continue
		}
		if err := copyZipEntry(writer, entry, normalized); err != nil {
			return err
		}
	}
	return nil
}

func addStandaloneFile(writer *zip.Writer, artifactPath string, overlays map[string]OverlayFile) error {
	entryName := normalizeZipEntryPath(filepath.Base(artifactPath))
	if entryName == "" {
		entryName = "agent.exe"
	}
	if _, overridden := overlays[entryName]; overridden {
		return nil
	}
	info, err := os.Stat(artifactPath)
	if err != nil {
		return fmt.Errorf("failed to stat artifact: %w", err)
	}
	mode := info.Mode()
	if mode == 0 {
		mode = 0o755
	}
	header := &zip.FileHeader{
		Name:    entryName,
		Method:  zip.Deflate,
		Comment: "wrapped executable",
	}
	header.SetMode(mode)
	header.Modified = info.ModTime()
	entryWriter, err := writer.CreateHeader(header)
	if err != nil {
		return fmt.Errorf("failed to add executable entry: %w",
			err)
	}
	file, err := os.Open(artifactPath)
	if err != nil {
		return fmt.Errorf("failed to open artifact: %w", err)
	}
	defer file.Close()
	if _, err := io.Copy(entryWriter, file); err != nil {
		return fmt.Errorf("failed to write executable: %w", err)
	}
	return nil
}

func copyZipEntry(writer *zip.Writer, entry *zip.File, normalized string) error {
	header := &zip.FileHeader{
		Name:           normalized,
		Method:         entry.Method,
		Flags:          entry.Flags,
		NonUTF8:        entry.NonUTF8,
		Modified:       entry.Modified,
		ModifiedTime:   entry.ModifiedTime,
		ModifiedDate:   entry.ModifiedDate,
		ExternalAttrs:  entry.ExternalAttrs,
		CreatorVersion: entry.CreatorVersion,
		ReaderVersion:  entry.ReaderVersion,
		Extra:          entry.Extra,
		Comment:        entry.Comment,
	}
	if fi := entry.FileInfo(); fi != nil {
		header.SetMode(fi.Mode())
		if fi.IsDir() && !strings.HasSuffix(header.Name, "/") {
			header.Name += "/"
		}
	}
	writerEntry, err := writer.CreateHeader(header)
	if err != nil {
		return fmt.Errorf("failed to create zip header for %s: %w", normalized, err)
	}
	if entry.FileInfo() != nil && entry.FileInfo().IsDir() {
		return nil
	}
	r, err := entry.Open()
	if err != nil {
		return fmt.Errorf("failed to open zip entry %s: %w", entry.Name, err)
	}
	defer r.Close()
	if _, err := io.Copy(writerEntry, r); err != nil {
		return fmt.Errorf("failed to copy zip entry %s: %w", entry.Name, err)
	}
	return nil
}

func addOverlayFilesToZip(writer *zip.Writer, overlays map[string]OverlayFile) error {
	for path, overlay := range overlays {
		header := &zip.FileHeader{
			Name:   path,
			Method: zip.Deflate,
		}
		mode := overlay.Mode
		if mode == 0 {
			mode = 0o640
		}
		header.SetMode(mode)
		header.Modified = time.Now().UTC()
		if strings.HasSuffix(path, "/") {
			if _, err := writer.CreateHeader(header); err != nil {
				return fmt.Errorf("failed to create directory entry %s: %w", path, err)
			}
			continue
		}
		entryWriter, err := writer.CreateHeader(header)
		if err != nil {
			return fmt.Errorf("failed to add overlay %s: %w", path, err)
		}
		if _, err := entryWriter.Write(overlay.Data); err != nil {
			return fmt.Errorf("failed to write overlay %s: %w", path, err)
		}
	}
	return nil
}

func normalizeZipEntryPath(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	clean := path.Clean(name)
	clean = strings.TrimPrefix(clean, "./")
	if clean == "." || clean == "" {
		return ""
	}
	if strings.HasPrefix(clean, "../") {
		return ""
	}
	return clean
}

func buildOutputFilename(input BuildInput, extension string) string {
	short := input.ConfigHash
	if short == "" {
		short = "config"
	}
	if len(short) > 8 {
		short = short[:8]
	}
	parts := []string{
		strings.TrimSpace(input.Component),
		strings.TrimSpace(input.Version),
		strings.TrimSpace(input.Platform),
		strings.TrimSpace(input.Arch),
		strings.TrimSpace(input.Format),
		short,
	}
	for i := range parts {
		if parts[i] == "" {
			parts[i] = "x"
		}
	}
	basename := strings.Join(parts, "-")
	return basename + extension
}
