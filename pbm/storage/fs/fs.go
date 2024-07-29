package fs

import (
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/percona/percona-backup-mongodb/pbm/errors"
	"github.com/percona/percona-backup-mongodb/pbm/storage"
)

type Conf struct {
	Path string `bson:"path" json:"path" yaml:"path"`
}

func (c *Conf) Cast() error {
	if c.Path == "" {
		return errors.New("path can't be empty")
	}

	return nil
}

type FS struct {
	root string
}

func New(opts Conf) (*FS, error) {
	info, err := os.Lstat(opts.Path)
	if err != nil {
		if os.IsNotExist(err) {
			if err := os.MkdirAll(opts.Path, os.ModeDir|0o755); err != nil {
				return nil, errors.Wrapf(err, "mkdir %s", opts.Path)
			}

			return &FS{opts.Path}, nil
		}

		return nil, errors.Wrapf(err, "stat %s", opts.Path)
	}

	root := opts.Path
	if info.Mode()&os.ModeSymlink != 0 {
		root, err = filepath.EvalSymlinks(opts.Path)
		if err != nil {
			return nil, errors.Wrapf(err, "resolve link: %s", opts.Path)
		}
		info, err = os.Lstat(root)
		if err != nil {
			return nil, errors.Wrapf(err, "stat %s", root)
		}
	}
	if !info.Mode().IsDir() {
		return nil, errors.Errorf("%s is not directory", root)
	}

	return &FS{root}, nil
}

func (*FS) Type() storage.Type {
	return storage.Filesystem
}

func WriteSync(filepath string, data io.Reader) error {
	err := os.MkdirAll(path.Dir(filepath), os.ModeDir|0o755)
	if err != nil {
		return errors.Wrapf(err, "create path %s", path.Dir(filepath))
	}

	fw, err := os.Create(filepath)
	if err != nil {
		return errors.Wrapf(err, "create destination file <%s>", filepath)
	}
	defer fw.Close()

	err = os.Chmod(filepath, 0o644)
	if err != nil {
		return errors.Wrapf(err, "change permissions for file <%s>", filepath)
	}

	_, err = io.Copy(fw, data)
	if err != nil {
		return errors.Wrapf(err, "copy file <%s>", filepath)
	}

	err = fw.Sync()
	return errors.Wrapf(err, "sync file <%s>", filepath)
}


func (fs *FS) Save(name string, data io.Reader, _ int64) error {
	filepath := path.Join(fs.root, name+".tmp")
	finalpath := path.Join(fs.root, name)

	err := WriteSync(filepath, data)
	if err != nil {
		os.Remove(filepath)
		return errors.Wrapf(err, "write-sync %s", path.Dir(filepath))
	}

	err = os.Rename(filepath, finalpath)
	return errors.Wrapf(err, "rename <%s> to <%s>", filepath, finalpath)
}

func (fs *FS) SourceReader(name string) (io.ReadCloser, error) {
	filepath := path.Join(fs.root, name)
	fr, err := os.Open(filepath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, storage.ErrNotExist
	}
	return fr, errors.Wrapf(err, "open file '%s'", filepath)
}

func (fs *FS) FileStat(name string) (storage.FileInfo, error) {
	inf := storage.FileInfo{}

	f, err := os.Stat(path.Join(fs.root, name))
	if errors.Is(err, os.ErrNotExist) {
		return inf, storage.ErrNotExist
	}
	if err != nil {
		return inf, err
	}

	inf.Size = f.Size()

	if inf.Size == 0 {
		return inf, storage.ErrEmpty
	}

	return inf, nil
}

func (fs *FS) List(prefix, suffix string) ([]storage.FileInfo, error) {
	var files []storage.FileInfo

	base := filepath.Join(fs.root, prefix)
	err := filepath.WalkDir(base, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return errors.Wrap(err, "walking the path")
		}

		info, _ := entry.Info()
		if info.IsDir() {
			return nil
		}

		f := filepath.ToSlash(strings.TrimPrefix(path, base))
		if len(f) == 0 {
			return nil
		}
		if f[0] == '/' {
			f = f[1:]
		}
		if strings.HasSuffix(f, suffix) {
			files = append(files, storage.FileInfo{Name: f, Size: info.Size()})
		}
		return nil
	})

	return files, err
}

func (fs *FS) Copy(src, dst string) error {
	from, err := os.Open(path.Join(fs.root, src))
	if err != nil {
		return errors.Wrap(err, "open src")
	}

	destFilename := path.Join(fs.root, dst+".tmp")
	finalFilename := path.Join(fs.root, dst)

	err = WriteSync(destFilename, from)
	if err != nil {
		os.Remove(destFilename)
		return errors.Wrapf(err, "write-sync %s", path.Dir(destFilename))
	}

	err = os.Rename(destFilename, finalFilename)
	return errors.Wrapf(err, "rename <%s> to <%s>", destFilename, finalFilename)
}

// Delete deletes given file from FS.
// It returns storage.ErrNotExist if a file isn't exists
func (fs *FS) Delete(name string) error {
	err := os.RemoveAll(path.Join(fs.root, name))
	if os.IsNotExist(err) {
		return storage.ErrNotExist
	}
	return err
}
