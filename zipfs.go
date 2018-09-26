package zipfs

import (
	"archive/zip"
	"bytes"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"strings"
	"time"
)

func NewZipFS(z *zip.Reader) http.FileSystem {
	t := newTrie()
	rootDir := &zipRoot{
		zipDir: zipDir{},
		Info:   zipRootInfo{time.Now()},
	}
	for _, entry := range z.File {
		if entry.Mode().IsDir() {
			// fake directory.
			dir := &zipDir{Info: entry.FileHeader}
			for _, subentry := range z.File {
				if strings.HasPrefix(subentry.Name, entry.Name) && subentry != entry &&
					len(strings.Split(strings.TrimRight(strings.TrimPrefix(subentry.Name, entry.Name), "/"), "/")) == 1 {
					clone := *subentry
					clone.Name = subentry.Name[len(entry.Name):]
					dir.Files = append(dir.Files, &clone)
				}
			}
			t.Add("/"+strings.TrimRight(entry.Name, "/"), *dir)
		} else {
			t.Add("/"+entry.Name, entry)
		}
		if len(strings.Split(strings.TrimRight(entry.Name, "/"), "/")) == 1 {
			clone := *entry
			rootDir.Files = append(rootDir.Files, &clone)
		}
	}
	t.Add("/", *rootDir)

	return &zipFS{
		zip:  z,
		trie: t,
	}
}

type zipFS struct {
	zip  *zip.Reader
	trie *trie
}

func (fs *zipFS) Open(name string) (http.File, error) {
	node, found := fs.trie.Find(name)
	if !found {
		return nil, os.ErrNotExist
	}

	switch entry := node.meta.(type) {
	case *zip.File:
		f, err := entry.Open()
		if err != nil {
			return nil, err
		}
		rawData, err := ioutil.ReadAll(f)
		if err != nil {
			return nil, err
		}
		f.Close()
		return &zipFile{
			Info: entry.FileHeader,
			Data: bytes.NewReader(rawData),
		}, nil
	case zipDir:
		return &entry, nil
	case zipRoot:
		return &entry, nil
	}

	return nil, os.ErrNotExist
}

type zipFile struct {
	Info zip.FileHeader
	Data io.ReadSeeker
}

func (f *zipFile) Close() error                              { return nil }
func (f *zipFile) Stat() (os.FileInfo, error)                { return f.Info.FileInfo(), nil }
func (f *zipFile) Readdir(count int) ([]os.FileInfo, error)  { return nil, os.ErrInvalid }
func (f *zipFile) Read(s []byte) (int, error)                { return f.Data.Read(s) }
func (f *zipFile) Seek(off int64, whence int) (int64, error) { return f.Data.Seek(off, whence) }

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
