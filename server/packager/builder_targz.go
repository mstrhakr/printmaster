package packager

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// TarGzBuilder injects overlay files into tarball-based artifacts (.tar.gz / .tgz).
type TarGzBuilder struct{}

// NewTarGzBuilder creates a builder capable of handling tar.gz artifacts.
func NewTarGzBuilder() *TarGzBuilder { return &TarGzBuilder{} }

// Format returns the identifier for this builder.
func (b *TarGzBuilder) Format() string { return "tar.gz" }

// Build produces a tarball with overlay files applied.
func (b *TarGzBuilder) Build(ctx context.Context, input BuildInput) (*BuildResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	lowerPath := strings.ToLower(input.ArtifactPath)
	if !(strings.HasSuffix(lowerPath, ".tar.gz") || strings.HasSuffix(lowerPath, ".tgz")) {
		return nil, fmt.Errorf("tar.gz builder requires .tar.gz artifact (got %s)", filepath.Ext(input.ArtifactPath))
	}
	if err := os.MkdirAll(input.OutputDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to ensure output directory: %w", err)
	}
	outPath := filepath.Join(input.OutputDir, buildOutputFilename(input, ".tar.gz"))

	srcFile, err := os.Open(input.ArtifactPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open artifact: %w", err)
	}
	defer srcFile.Close()

	reader, err := gzip.NewReader(srcFile)
	if err != nil {
		return nil, fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer reader.Close()

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

	gzipWriter := gzip.NewWriter(outFile)
	defer gzipWriter.Close()
	tarWriter := tar.NewWriter(gzipWriter)
	defer tarWriter.Close()

	tarReader := tar.NewReader(reader)
	overlayMap := make(map[string]OverlayFile, len(input.OverlayFiles))
	for _, overlay := range input.OverlayFiles {
		overlayMap[overlay.Path] = overlay
	}
	writtenDirs := make(map[string]struct{})

	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		hdr, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read tar entry: %w", err)
		}
		normalized := normalizeTarPath(hdr.Name)
		if normalized == "" {
			if err := discardTarEntry(tarReader, hdr); err != nil {
				return nil, err
			}
			continue
		}
		if _, overridden := overlayMap[normalized]; overridden && isRegularTarFile(hdr) {
			if err := discardTarEntry(tarReader, hdr); err != nil {
				return nil, err
			}
			continue
		}
		hdrCopy := *hdr
		hdrCopy.Name = normalized
		if err := tarWriter.WriteHeader(&hdrCopy); err != nil {
			return nil, fmt.Errorf("failed to write tar header %s: %w", normalized, err)
		}
		if isTarRegularType(hdrCopy.Typeflag) {
			if _, err := io.CopyN(tarWriter, tarReader, hdrCopy.Size); err != nil {
				return nil, fmt.Errorf("failed to copy tar entry %s: %w", normalized, err)
			}
		} else {
			if err := discardTarEntry(tarReader, hdr); err != nil {
				return nil, err
			}
		}
		markDirectories(normalized, writtenDirs)
	}

	if err := addOverlayFilesToTar(tarWriter, overlayMap, writtenDirs); err != nil {
		return nil, err
	}

	if err := tarWriter.Close(); err != nil {
		return nil, err
	}
	if err := gzipWriter.Close(); err != nil {
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
		"builder":         "tar.gz",
		"source_artifact": filepath.Base(input.ArtifactPath),
		"overlay_count":   len(input.OverlayFiles),
	}
	return &BuildResult{BundlePath: outPath, SizeBytes: info.Size(), Metadata: meta}, nil
}

func normalizeTarPath(name string) string {
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

func discardTarEntry(r *tar.Reader, hdr *tar.Header) error {
	if hdr == nil || hdr.Size == 0 {
		return nil
	}
	_, err := io.CopyN(io.Discard, r, hdr.Size)
	return err
}

func isRegularTarFile(hdr *tar.Header) bool {
	return hdr != nil && isTarRegularType(hdr.Typeflag)
}

func isTarRegularType(flag byte) bool {
	return flag == tar.TypeReg || flag == 0 // tar.TypeRegA (legacy) encoded as NUL
}

func addOverlayFilesToTar(writer *tar.Writer, overlays map[string]OverlayFile, writtenDirs map[string]struct{}) error {
	for path, overlay := range overlays {
		if err := ensureTarDirs(writer, path, writtenDirs); err != nil {
			return err
		}
		mode := overlay.Mode
		if mode == 0 {
			mode = 0o640
		}
		hdr := &tar.Header{
			Name:     path,
			Mode:     int64(mode.Perm()),
			Size:     int64(len(overlay.Data)),
			ModTime:  time.Now().UTC(),
			Typeflag: tar.TypeReg,
		}
		if err := writer.WriteHeader(hdr); err != nil {
			return fmt.Errorf("failed to write overlay header %s: %w", path, err)
		}
		if _, err := writer.Write(overlay.Data); err != nil {
			return fmt.Errorf("failed to write overlay data %s: %w", path, err)
		}
	}
	return nil
}

func ensureTarDirs(writer *tar.Writer, filePath string, written map[string]struct{}) error {
	dir := path.Dir(filePath)
	if dir == "." || dir == "" {
		return nil
	}
	segments := strings.Split(dir, "/")
	var current string
	for _, segment := range segments {
		if segment == "" {
			continue
		}
		if current == "" {
			current = segment
		} else {
			current = current + "/" + segment
		}
		if _, ok := written[current]; ok {
			continue
		}
		hdr := &tar.Header{
			Name:     current + "/",
			Mode:     0o755,
			Typeflag: tar.TypeDir,
			ModTime:  time.Now().UTC(),
		}
		if err := writer.WriteHeader(hdr); err != nil {
			return fmt.Errorf("failed to write directory header %s: %w", current, err)
		}
		written[current] = struct{}{}
	}
	return nil
}

func markDirectories(p string, written map[string]struct{}) {
	dir := strings.TrimSuffix(p, "/")
	if dir == p {
		dir = path.Dir(p)
	}
	if dir == "." || dir == "" || dir == "/" {
		return
	}
	parts := strings.Split(dir, "/")
	var current string
	for _, part := range parts {
		if part == "" {
			continue
		}
		if current == "" {
			current = part
		} else {
			current = current + "/" + part
		}
		written[current] = struct{}{}
	}
}
