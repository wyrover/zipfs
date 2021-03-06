/*
Package that host the content of a zip via Google net/http.

Example:

	package main

	import (
		"fmt"
		"log"
		"net/http"

		"github.com/cjtoolkit/zipfs"
	)

	func main() {
		// Init ZipFS, if the zip file does not exist it will fallback to embedded zip file within application, if that
		// also does not exist, it will panic.
		fs := zipfs.InitZipFs("asset.zip")

		fmt.Println("Running ZipFS via http server on port 8080.")
		log.Print(http.ListenAndServe(":8080", http.FileServer(fs)))
	}

To embed zip to the application, first create a zip file with your preferred achiever with compression disabled.
Than append the content of the zip to the compiled application.  You can use this simple shell command to append.

	$ cat asset.zip >> application

*/
package zipfs

import (
	"archive/zip"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Create Zip File System, just from the zip reader, with seek disabled.
func NewZipFS(z *zip.Reader) http.FileSystem { return NewZipFSWithReaderAt(z, nil) }

// Create Zip File System, from the zip reader and readerAt.
// If readerAt is nil, than seeking will be disabled.
func NewZipFSWithReaderAt(z *zip.Reader, readerAt io.ReaderAt) http.FileSystem {
	trie := newTrie()
	rootDir := &zipRoot{
		zipDir: zipDir{},
		Info:   zipRootInfo{time.Now()},
	}
	dirs := []*zip.File{}
	for _, entry := range z.File {
		if entry.Mode().IsDir() {
			dirs = append(dirs, entry)
			trie.Add("/"+strings.TrimRight(entry.Name, "/"), entry)
		} else {
			trie.Add("/"+entry.Name, entry)
		}
		if len(strings.Split(strings.TrimRight(entry.Name, "/"), "/")) == 1 {
			clone := *entry
			rootDir.Files = append(rootDir.Files, &clone)
		}
	}
	for _, entry := range dirs {
		// fake directory.
		dir := &zipDir{Info: entry.FileHeader}
		name := "/" + strings.TrimRight(entry.Name, "/")
		for _, dirContent := range trie.PrefixSearch(name) {
			if !strings.HasPrefix(dirContent, "/"+entry.Name) ||
				len(strings.Split(strings.TrimRight(strings.TrimPrefix(dirContent, "/"+entry.Name), "/"), "/")) > 1 {
				continue
			}
			node, _ := trie.Find(dirContent)
			subentry := node.meta.(*zip.File)
			clone := *subentry
			clone.Name = subentry.Name[len(entry.Name):]
			dir.Files = append(dir.Files, &clone)
		}

		trie.Add(name, *dir)
	}
	trie.Add("/", *rootDir)

	return &zipFS{
		zip:      z,
		readerAt: readerAt,
		trie:     trie,
	}
}

type zipFS struct {
	zip      *zip.Reader
	readerAt io.ReaderAt
	trie     *trie
}

func (fs *zipFS) Open(name string) (http.File, error) {
	if !strings.HasPrefix(name, "/") {
		return nil, os.ErrNotExist
	}
	node, found := fs.trie.Find(name)
	if !found {
		return nil, os.ErrNotExist
	}

	switch entry := node.meta.(type) {
	case *zip.File:
		return fs.processZipFile(entry)
	case zipDir:
		return &entry, nil
	case zipRoot:
		return &entry, nil
	}

	return nil, os.ErrNotExist
}

func (fs *zipFS) processZipFile(entry *zip.File) (http.File, error) {
	if fs.readerAt != nil && entry.Method == zip.Store {
		offset, err := entry.DataOffset()
		if err != nil {
			return nil, err
		}
		return &uncompressedFile{
			SectionReader: io.NewSectionReader(fs.readerAt, offset, int64(entry.UncompressedSize64)),
			zipFile:       entry,
		}, nil
	}
	ff, err := entry.Open()
	if err != nil {
		return nil, err
	}
	return &compressedFile{
		ReadCloser: ff,
		zipFile:    entry,
	}, nil
}

type uncompressedFile struct {
	*io.SectionReader
	zipFile *zip.File
}

func (f *uncompressedFile) Close() error               { return nil }
func (f *uncompressedFile) Stat() (os.FileInfo, error) { return f.zipFile.FileInfo(), nil }

func (f *uncompressedFile) Readdir(count int) ([]os.FileInfo, error) {
	return nil, errors.New("not a directory")
}

type compressedFile struct {
	io.ReadCloser
	zipFile *zip.File
}

func (f *compressedFile) Seek(offset int64, whence int) (int64, error) {
	return -1, errors.New("seek on compressed file")
}

func (f *compressedFile) Readdir(count int) ([]os.FileInfo, error) {
	return nil, errors.New("not a directory")
}

func (f *compressedFile) Stat() (os.FileInfo, error) {
	return f.zipFile.FileInfo(), nil
}

type zipDir struct {
	Info  zip.FileHeader
	Files []*zip.File
}

func (f *zipDir) Close() error                              { return nil }
func (f *zipDir) Stat() (os.FileInfo, error)                { return f.Info.FileInfo(), nil }
func (f *zipDir) Read(s []byte) (int, error)                { return 0, os.ErrInvalid }
func (f *zipDir) Seek(off int64, whence int) (int64, error) { return 0, os.ErrInvalid }

func (f *zipDir) Readdir(count int) ([]os.FileInfo, error) {
	if len(f.Files) == 0 {
		return nil, io.EOF
	}
	if count < 0 || count > len(f.Files) {
		count = len(f.Files)
	}
	infos := make([]os.FileInfo, count)
	for i, f := range f.Files {
		if i >= count {
			break
		}
		infos[i] = f.FileInfo()
	}
	f.Files = f.Files[count:]
	return infos, nil
}

type zipRootInfo struct {
	t time.Time
}

func (i zipRootInfo) Name() string       { return "/" }
func (i zipRootInfo) Size() int64        { return 0 }
func (i zipRootInfo) Mode() os.FileMode  { return os.ModeDir | 0777 }
func (i zipRootInfo) ModTime() time.Time { return i.t }
func (i zipRootInfo) IsDir() bool        { return true }
func (i zipRootInfo) Sys() interface{}   { return nil }

type zipRoot struct {
	zipDir
	Info zipRootInfo
}

func (f *zipRoot) Stat() (os.FileInfo, error) { return f.Info, nil }
