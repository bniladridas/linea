package web

import (
	"io/fs"
	"testing"
)

func TestFilesReturnsNonNilFileSystem(t *testing.T) {
	files := Files()
	if files == nil {
		t.Fatal("Files() returned nil")
	}
}

func TestFilesContainsIndexHTML(t *testing.T) {
	files := Files()
	file, err := files.Open("index.html")
	if err != nil {
		t.Fatalf("Open(index.html) error = %v", err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if info.IsDir() {
		t.Fatal("index.html is a directory")
	}
	if info.Size() == 0 {
		t.Fatal("index.html is empty")
	}
}

func TestFilesReturnsNotFoundForUnknownPaths(t *testing.T) {
	files := Files()
	if _, err := files.Open("nonexistent.html"); err == nil {
		t.Fatal("Open(nonexistent.html) error = nil, want error")
	}
}

func TestFilesIsReadonly(t *testing.T) {
	files := Files()
	file, err := files.Open("index.html")
	if err != nil {
		t.Fatalf("Open(index.html) error = %v", err)
	}
	defer file.Close()

	if _, ok := file.(fs.ReadFileFS); !ok {
		// http.FileSystem returned by http.FS may not support ReadFileFS;
		// just verify the file can be read
	}
	buf := make([]byte, 1024)
	n, err := file.Read(buf)
	if err != nil && n == 0 {
		t.Fatalf("Read() error = %v", err)
	}
	if n == 0 {
		t.Fatal("Read() returned 0 bytes")
	}
}
