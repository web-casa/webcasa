package filemanager

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const maxExtractSize = 1 << 30 // 1 GB total extraction limit
func (f *FileOps) Compress(paths []string, destPath, format string) error {
	absDest, err := f.safePath(destPath)
	if err != nil {
		return err
	}

	absPaths := make([]string, 0, len(paths))
	for _, p := range paths {
		abs, err := f.safePath(p)
		if err != nil {
			return fmt.Errorf("invalid path %q: %w", p, err)
		}
		absPaths = append(absPaths, abs)
	}

	switch format {
	case "tar.gz", "tgz":
		return f.compressTarGz(absPaths, absDest)
	case "zip":
		return f.compressZip(absPaths, absDest)
	default:
		return fmt.Errorf("unsupported format: %s (use tar.gz or zip)", format)
	}
}

// Extract extracts an archive to destDir. Format is auto-detected.
func (f *FileOps) Extract(archivePath, destDir string) error {
	absSrc, err := f.safePath(archivePath)
	if err != nil {
		return err
	}
	absDest, err := f.safePath(destDir)
	if err != nil {
		return err
	}

	lower := strings.ToLower(absSrc)
	switch {
	case strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz"):
		return f.extractTarGz(absSrc, absDest)
	case strings.HasSuffix(lower, ".zip"):
		return f.extractZip(absSrc, absDest)
	default:
		return fmt.Errorf("unsupported archive format")
	}
}

func (f *FileOps) compressTarGz(sources []string, dest string) error {
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()

	gw := gzip.NewWriter(out)
	defer gw.Close()
	tw := tar.NewWriter(gw)
	defer tw.Close()

	for _, src := range sources {
		if err := filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			// Skip symlinks and non-regular files
			if info.Mode()&os.ModeSymlink != 0 || (!info.IsDir() && !info.Mode().IsRegular()) {
				return nil
			}
			header, err := tar.FileInfoHeader(info, "")
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(filepath.Dir(src), path)
			header.Name = rel
			if err := tw.WriteHeader(header); err != nil {
				return err
			}
			if info.IsDir() {
				return nil
			}
			file, err := os.Open(path)
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(tw, file)
			file.Close() // Close immediately, don't defer
			return copyErr
		}); err != nil {
			return err
		}
	}
	return nil
}

func (f *FileOps) compressZip(sources []string, dest string) error {
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()

	zw := zip.NewWriter(out)
	defer zw.Close()

	for _, src := range sources {
		if err := filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			// Skip symlinks and non-regular files
			if info.Mode()&os.ModeSymlink != 0 || (!info.IsDir() && !info.Mode().IsRegular()) {
				return nil
			}
			rel, _ := filepath.Rel(filepath.Dir(src), path)
			if info.IsDir() {
				_, err := zw.Create(rel + "/")
				return err
			}
			w, err := zw.Create(rel)
			if err != nil {
				return err
			}
			file, err := os.Open(path)
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(w, file)
			file.Close() // Close immediately, don't defer
			return copyErr
		}); err != nil {
			return err
		}
	}
	return nil
}

func (f *FileOps) extractTarGz(src, dest string) error {
	file, err := os.Open(src)
	if err != nil {
		return err
	}
	defer file.Close()

	gr, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gr.Close()

	var totalWritten int64
	tr := tar.NewReader(gr)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		target := filepath.Join(dest, header.Name)
		// Zip-slip protection.
		if !strings.HasPrefix(filepath.Clean(target)+"/", filepath.Clean(dest)+"/") {
			return fmt.Errorf("illegal path in archive: %s", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			os.MkdirAll(target, 0755)
		case tar.TypeReg:
			if header.Size > maxExtractSize-totalWritten {
				return fmt.Errorf("archive too large (exceeds %d bytes limit)", maxExtractSize)
			}
			os.MkdirAll(filepath.Dir(target), 0755)
			out, err := os.Create(target)
			if err != nil {
				return err
			}
			written, err := io.Copy(out, io.LimitReader(tr, maxExtractSize-totalWritten+1))
			out.Close()
			if err != nil {
				return err
			}
			totalWritten += written
			if totalWritten > maxExtractSize {
				return fmt.Errorf("archive too large (exceeds %d bytes limit)", maxExtractSize)
			}
			os.Chmod(target, os.FileMode(header.Mode)&0777) // Strip setuid/setgid
		case tar.TypeSymlink:
			// Skip symlinks for security
			continue
		default:
			// Skip hardlinks and other special types
			continue
		}
	}
	return nil
}

func (f *FileOps) extractZip(src, dest string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()

	var totalWritten int64
	for _, zf := range r.File {
		target := filepath.Join(dest, zf.Name)
		// Zip-slip protection.
		if !strings.HasPrefix(filepath.Clean(target)+"/", filepath.Clean(dest)+"/") {
			return fmt.Errorf("illegal path in archive: %s", zf.Name)
		}

		if zf.FileInfo().IsDir() {
			os.MkdirAll(target, 0755)
			continue
		}
		// Check uncompressed size before extracting
		if int64(zf.UncompressedSize64) > maxExtractSize-totalWritten {
			return fmt.Errorf("archive too large (exceeds %d bytes limit)", maxExtractSize)
		}
		os.MkdirAll(filepath.Dir(target), 0755)
		out, err := os.Create(target)
		if err != nil {
			return err
		}
		rc, err := zf.Open()
		if err != nil {
			out.Close()
			return err
		}
		written, err := io.Copy(out, io.LimitReader(rc, maxExtractSize-totalWritten+1))
		rc.Close()
		out.Close()
		if err != nil {
			return err
		}
		totalWritten += written
		if totalWritten > maxExtractSize {
			return fmt.Errorf("archive too large (exceeds %d bytes limit)", maxExtractSize)
		}
	}
	return nil
}
