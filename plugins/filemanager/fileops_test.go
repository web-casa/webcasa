package filemanager

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSafePath_Normal(t *testing.T) {
	root := t.TempDir()
	ops := NewFileOps(root)

	got, err := ops.safePath("/foo/bar")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(root, "foo/bar")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSafePath_Traversal(t *testing.T) {
	root := t.TempDir()
	ops := NewFileOps(root)

	// filepath.Clean normalizes ../ so direct traversal via ../ is inherently safe.
	// The real attack vector is symlinks. Create a symlink pointing outside root.
	os.Symlink("/etc", filepath.Join(root, "escape"))

	// Trying to access through the symlink should fail.
	_, err := ops.safePath("/escape/passwd")
	if err == nil {
		t.Fatal("expected error for symlink traversal")
	}
}

func TestList(t *testing.T) {
	root := t.TempDir()
	ops := NewFileOps(root)

	os.MkdirAll(filepath.Join(root, "subdir"), 0755)
	os.WriteFile(filepath.Join(root, "file.txt"), []byte("hello"), 0644)

	entries, err := ops.List("/")
	if err != nil {
		t.Fatalf("list error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	names := map[string]bool{}
	for _, e := range entries {
		names[e.Name] = true
	}
	if !names["subdir"] || !names["file.txt"] {
		t.Errorf("unexpected entries: %v", names)
	}
}

func TestReadWrite(t *testing.T) {
	root := t.TempDir()
	ops := NewFileOps(root)

	content := "hello world\nline2"
	if err := ops.Write("/test.txt", content); err != nil {
		t.Fatalf("write error: %v", err)
	}

	got, err := ops.Read("/test.txt")
	if err != nil {
		t.Fatalf("read error: %v", err)
	}
	if got != content {
		t.Errorf("got %q, want %q", got, content)
	}
}

func TestMkdirAndDelete(t *testing.T) {
	root := t.TempDir()
	ops := NewFileOps(root)

	if err := ops.Mkdir("/a/b/c"); err != nil {
		t.Fatalf("mkdir error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "a/b/c")); err != nil {
		t.Fatalf("dir not created: %v", err)
	}

	if err := ops.Delete("/a"); err != nil {
		t.Fatalf("delete error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "a")); !os.IsNotExist(err) {
		t.Fatal("dir should be deleted")
	}
}

func TestRename(t *testing.T) {
	root := t.TempDir()
	ops := NewFileOps(root)

	ops.Write("/old.txt", "data")
	if err := ops.Rename("/old.txt", "/new.txt"); err != nil {
		t.Fatalf("rename error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "old.txt")); !os.IsNotExist(err) {
		t.Fatal("old file should not exist")
	}
	got, err := ops.Read("/new.txt")
	if err != nil || got != "data" {
		t.Fatalf("renamed file content mismatch: %v %q", err, got)
	}
}

func TestChmod(t *testing.T) {
	root := t.TempDir()
	ops := NewFileOps(root)

	ops.Write("/perm.txt", "test")
	if err := ops.Chmod("/perm.txt", 0600); err != nil {
		t.Fatalf("chmod error: %v", err)
	}
	info, _ := os.Stat(filepath.Join(root, "perm.txt"))
	if info.Mode().Perm() != 0600 {
		t.Errorf("got mode %v, want 0600", info.Mode().Perm())
	}
}

func TestStat(t *testing.T) {
	root := t.TempDir()
	ops := NewFileOps(root)

	ops.Write("/info.txt", "hello")
	fi, err := ops.Stat("/info.txt")
	if err != nil {
		t.Fatalf("stat error: %v", err)
	}
	if fi.Name != "info.txt" || fi.IsDir || fi.Size != 5 {
		t.Errorf("unexpected stat result: %+v", fi)
	}
}

func TestDeleteRoot(t *testing.T) {
	root := t.TempDir()
	ops := NewFileOps(root)

	err := ops.Delete("/")
	if err == nil {
		t.Fatal("should not be able to delete root")
	}
}

func TestCompressExtractTarGz(t *testing.T) {
	root := t.TempDir()
	ops := NewFileOps(root)

	// Create test files.
	ops.Mkdir("/src")
	ops.Write("/src/a.txt", "aaa")
	ops.Write("/src/b.txt", "bbb")

	// Compress.
	if err := ops.Compress([]string{"/src"}, "/archive.tar.gz", "tar.gz"); err != nil {
		t.Fatalf("compress tar.gz error: %v", err)
	}

	// Extract.
	ops.Mkdir("/out")
	if err := ops.Extract("/archive.tar.gz", "/out"); err != nil {
		t.Fatalf("extract tar.gz error: %v", err)
	}

	got, err := ops.Read("/out/src/a.txt")
	if err != nil || got != "aaa" {
		t.Fatalf("extracted content mismatch: %v %q", err, got)
	}
}

func TestCompressExtractZip(t *testing.T) {
	root := t.TempDir()
	ops := NewFileOps(root)

	ops.Mkdir("/zsrc")
	ops.Write("/zsrc/x.txt", "xxx")

	if err := ops.Compress([]string{"/zsrc"}, "/archive.zip", "zip"); err != nil {
		t.Fatalf("compress zip error: %v", err)
	}

	ops.Mkdir("/zout")
	if err := ops.Extract("/archive.zip", "/zout"); err != nil {
		t.Fatalf("extract zip error: %v", err)
	}

	got, err := ops.Read("/zout/zsrc/x.txt")
	if err != nil || got != "xxx" {
		t.Fatalf("extracted content mismatch: %v %q", err, got)
	}
}
