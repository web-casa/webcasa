package filemanager

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const maxReadSize = 10 << 20 // 10 MB

// FileOps provides safe file system operations within a root path.
type FileOps struct {
	rootPath string
}

// NewFileOps creates a FileOps scoped to the given root.
func NewFileOps(rootPath string) *FileOps {
	return &FileOps{rootPath: filepath.Clean(rootPath)}
}

// FileInfo describes a file or directory.
type FileInfo struct {
	Name      string    `json:"name"`
	Path      string    `json:"path"`
	Size      int64     `json:"size"`
	Mode      string    `json:"mode"`
	ModeOctal string    `json:"mode_octal"`
	ModTime   time.Time `json:"mod_time"`
	IsDir     bool      `json:"is_dir"`
	IsSymlink bool      `json:"is_symlink"`
	User      string    `json:"user"`
	Group     string    `json:"group"`
}

// safePath resolves a request path and ensures it is within rootPath.
func (f *FileOps) safePath(reqPath string) (string, error) {
	cleaned := filepath.Clean("/" + reqPath)
	abs := filepath.Join(f.rootPath, cleaned)

	// Resolve the root path (might be a symlink itself, e.g., /tmp on macOS).
	rootResolved, err := filepath.EvalSymlinks(f.rootPath)
	if err != nil {
		rootResolved = f.rootPath
	}

	// If root is "/" everything is inside it — skip traversal checks.
	if rootResolved == "/" {
		return abs, nil
	}

	// For existing parent directories, resolve symlinks and check containment.
	// Walk up until we find an existing directory.
	check := abs
	for {
		resolved, err := filepath.EvalSymlinks(check)
		if err == nil {
			if resolved != rootResolved && !strings.HasPrefix(resolved+"/", rootResolved+"/") {
				return "", fmt.Errorf("access denied: path outside root")
			}
			break
		}
		parent := filepath.Dir(check)
		if parent == check {
			break // reached filesystem root
		}
		check = parent
	}

	return abs, nil
}

// List returns entries in a directory.
func (f *FileOps) List(reqPath string) ([]FileInfo, error) {
	abs, err := f.safePath(reqPath)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		return nil, err
	}

	result := make([]FileInfo, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		fi := f.buildFileInfo(filepath.Join(abs, entry.Name()), info)
		fi.IsSymlink = entry.Type()&fs.ModeSymlink != 0
		result = append(result, fi)
	}
	return result, nil
}

// Read returns file content as string (max 10 MB).
func (f *FileOps) Read(reqPath string) (string, error) {
	abs, err := f.safePath(reqPath)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("cannot read directory")
	}
	if info.Size() > maxReadSize {
		return "", fmt.Errorf("file too large (max %d bytes)", maxReadSize)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// Write creates or overwrites a file.
func (f *FileOps) Write(reqPath, content string) error {
	abs, err := f.safePath(reqPath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
		return err
	}
	return os.WriteFile(abs, []byte(content), 0644)
}

// WriteFromReader streams content from a reader to a file on disk.
// This avoids buffering the entire file in memory for large uploads.
func (f *FileOps) WriteFromReader(reqPath string, r io.Reader, limit int64) error {
	abs, err := f.safePath(reqPath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
		return err
	}
	out, err := os.Create(abs)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, io.LimitReader(r, limit))
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

// Stat returns info for a single path.
func (f *FileOps) Stat(reqPath string) (*FileInfo, error) {
	abs, err := f.safePath(reqPath)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(abs)
	if err != nil {
		return nil, err
	}
	fi := f.buildFileInfo(abs, info)
	fi.IsSymlink = info.Mode()&fs.ModeSymlink != 0
	return &fi, nil
}

// Mkdir creates a directory (and parents).
func (f *FileOps) Mkdir(reqPath string) error {
	abs, err := f.safePath(reqPath)
	if err != nil {
		return err
	}
	return os.MkdirAll(abs, 0755)
}

// Delete removes a file or directory recursively.
func (f *FileOps) Delete(reqPath string) error {
	abs, err := f.safePath(reqPath)
	if err != nil {
		return err
	}
	// Prevent deleting root.
	rootResolved, err := filepath.EvalSymlinks(f.rootPath)
	if err != nil {
		rootResolved = f.rootPath
	}
	absResolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		absResolved = abs
	}
	if absResolved == rootResolved {
		return fmt.Errorf("cannot delete root directory")
	}
	return os.RemoveAll(abs)
}

// Rename moves/renames a file or directory.
func (f *FileOps) Rename(oldPath, newPath string) error {
	absOld, err := f.safePath(oldPath)
	if err != nil {
		return err
	}
	absNew, err := f.safePath(newPath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(absNew), 0755); err != nil {
		return err
	}
	return os.Rename(absOld, absNew)
}

// Chmod changes file permissions.
func (f *FileOps) Chmod(reqPath string, mode os.FileMode) error {
	abs, err := f.safePath(reqPath)
	if err != nil {
		return err
	}
	return os.Chmod(abs, mode)
}

// Download opens a file for streaming. Caller must close the reader.
func (f *FileOps) Download(reqPath string) (io.ReadCloser, string, int64, error) {
	abs, err := f.safePath(reqPath)
	if err != nil {
		return nil, "", 0, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, "", 0, err
	}
	if info.IsDir() {
		return nil, "", 0, fmt.Errorf("cannot download directory")
	}
	file, err := os.Open(abs)
	if err != nil {
		return nil, "", 0, err
	}
	return file, filepath.Base(abs), info.Size(), nil
}

func (f *FileOps) buildFileInfo(absPath string, info fs.FileInfo) FileInfo {
	// Relative path from root for API response.
	relPath, _ := filepath.Rel(f.rootPath, absPath)
	if !strings.HasPrefix(relPath, "/") {
		relPath = "/" + relPath
	}

	fi := FileInfo{
		Name:      info.Name(),
		Path:      relPath,
		Size:      info.Size(),
		Mode:      info.Mode().String(),
		ModeOctal: fmt.Sprintf("%04o", info.Mode().Perm()),
		ModTime:   info.ModTime(),
		IsDir:     info.IsDir(),
	}

	// Get owner/group from syscall.
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		if u, err := user.LookupId(strconv.Itoa(int(stat.Uid))); err == nil {
			fi.User = u.Username
		} else {
			fi.User = strconv.Itoa(int(stat.Uid))
		}
		if g, err := user.LookupGroupId(strconv.Itoa(int(stat.Gid))); err == nil {
			fi.Group = g.Name
		} else {
			fi.Group = strconv.Itoa(int(stat.Gid))
		}
	}
	return fi
}
