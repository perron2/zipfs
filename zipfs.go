package zipfs

import (
	"archive/zip"
	"encoding/binary"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// New creates a new zip file system. The specified name must correspond
// to a name used in a zipfs: comment. If the zipfs command has already
// been run on the executable, the zipped data is being read from there,
// otherwise (e.g. during development) it is being read from the specified
// directory.
func New(name string, dir string) http.FileSystem {
	return &gatewayFS{
		name: name,
		dir:  dir,
	}
}

type gatewayFS struct {
	name string
	dir  string
	fs   http.FileSystem
}

func (gs *gatewayFS) Open(name string) (http.File, error) {
	if gs.fs == nil {
		zip := gs.openZipFile(gs.name)
		if zip == nil {
			gs.fs = http.Dir(gs.dir)
		} else {
			gs.fs = &zipFS{zip: zip}
		}
	}
	return gs.fs.Open(name)
}

func (gs *gatewayFS) openZipFile(name string) *zip.Reader {
	name = name + "\x00"
	nameBuffer := make([]byte, len(name))

	f, err := os.Open(os.Args[0])
	if err != nil {
		return nil
	}

	endOffset, err := f.Seek(-8, os.SEEK_END)
	if err != nil {
		return nil
	}

	var offset int64
	for {
		var block struct {
			Tag    [4]byte
			Offset int32
		}

		err = binary.Read(f, binary.BigEndian, &block)
		if err != nil {
			return nil
		}

		if string(block.Tag[:]) != "ZIPR" {
			return nil
		}

		_, err = f.Seek(int64(block.Offset), os.SEEK_SET)
		if err != nil {
			return nil
		}

		_, err := f.Read(nameBuffer)
		if err != nil {
			return nil
		}

		if string(nameBuffer) == name {
			offset, _ = f.Seek(0, os.SEEK_CUR)
			break
		}

		endOffset, err = f.Seek(int64(block.Offset-8), os.SEEK_SET)
		if err != nil {
			return nil
		}
	}

	if offset == 0 {
		return nil
	}

	zip, err := zip.NewReader(&offsetReader{
		r:      f,
		offset: offset,
	}, endOffset-offset)
	if err != nil {
		f.Close()
		return nil
	}

	return zip
}

type offsetReader struct {
	r      io.ReaderAt
	offset int64
}

func (r *offsetReader) ReadAt(b []byte, off int64) (n int, err error) {
	return r.r.ReadAt(b, off+r.offset)
}

type zipFS struct {
	zip *zip.Reader
}

func (fs *zipFS) Open(name string) (http.File, error) {
	name = strings.TrimLeft(name, "/")
	for _, file := range fs.zip.File {
		if file.Name == name {
			return &zipFile{
				file: file,
			}, nil
		}
	}
	return nil, errors.New("File not found")
}

type zipFile struct {
	file *zip.File
	rc   io.ReadCloser
}

func (f *zipFile) Close() error {
	if f.rc != nil {
		err := f.rc.Close()
		f.rc = nil
		if err != nil {
			return err
		}
	}
	return nil
}

func (f *zipFile) Read(p []byte) (n int, err error) {
	if f.rc == nil {
		rc, err := f.file.Open()
		if err != nil {
			return 0, err
		}
		f.rc = rc
	}
	return f.rc.Read(p)
}

func (f *zipFile) Readdir(count int) ([]os.FileInfo, error) {
	var list []os.FileInfo
	return list, nil
}

func (f *zipFile) Seek(offset int64, whence int) (int64, error) {
	return 0, nil
}

func (f *zipFile) Stat() (os.FileInfo, error) {
	return f, nil
}

func (f *zipFile) Name() string {
	return f.file.Name
}

func (f *zipFile) Size() int64 {
	return int64(f.file.UncompressedSize64)
}

func (f *zipFile) Mode() os.FileMode {
	return f.file.Mode()
}

func (f *zipFile) ModTime() time.Time {
	return f.file.ModTime()
}

func (f *zipFile) IsDir() bool {
	return false
}

func (f *zipFile) Sys() interface{} {
	return nil
}
