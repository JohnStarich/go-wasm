package mountfs

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/johnstarich/go-wasm/internal/fsutil"
	"github.com/johnstarich/go-wasm/log"
	"github.com/pkg/errors"
	"github.com/spf13/afero"
)

type Fs struct {
	mounts []mount // When accessing mounts, always copy the slice ref. Changes must always re-slice and re-assign, never mutate
	mu     sync.RWMutex
}

type mount struct {
	path string
	fs   afero.Fs
}

// New creates a mountable afero.Fs. This means multiple Fs's can be overlayed on top of one another. Each mount is higher precedence than the last.
// NOTE: Does not support renaming across mount boundaries yet.
// NOTE: Currently Fs's mount paths are not trimmed off of the original Fs method call.
func New(defaultFs afero.Fs) *Fs {
	root := filepath.Clean(afero.FilePathSeparator) // TODO if contributing to afero, does this work on Windows?
	return &Fs{
		mounts: []mount{
			{path: root, fs: defaultFs},
		},
	}
}

func (m *Fs) Mounts() (pathsToFSName map[string]string) {
	pathsToFSName = make(map[string]string)
	mounts := m.mounts
	for _, mount := range mounts {
		pathsToFSName[mount.path] = mount.fs.Name()
	}
	return
}

type fsClearer interface {
	Clear() error
}

func (m *Fs) DestroyMount(path string) error {
	mount := m.mountForPath(path)
	if mount.path != path {
		return errors.Errorf("Mount not found for path: %s", path)
	}
	clearer, ok := mount.fs.(fsClearer)
	if !ok {
		return errors.Errorf("Destroy not supported for mount: %s", mount.fs.Name())
	}
	return clearer.Clear()
}

func (m *Fs) Mount(path string, fs afero.Fs) error {
	path = fsutil.NormalizePath(path)
	if path == afero.FilePathSeparator {
		return os.ErrExist
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, mount := range m.mounts {
		if mount.path == path {
			return os.ErrExist
		}
	}

	info, err := m.Stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return afero.ErrNotDir
	}
	m.mounts = append(m.mounts, mount{path: path, fs: fs})
	return nil
}

func (m *Fs) FSForPath(path string) afero.Fs {
	return mountedFs{m.mountForPath(path)}
}

func (m *Fs) mountForPath(path string) mount {
	path = fsutil.NormalizePath(path)
	mounts := m.mounts // copy slice for consistent reads
	for i := len(mounts) - 1; i >= 0; i-- {
		if path == mounts[i].path || strings.HasPrefix(path, mounts[i].path+afero.FilePathSeparator) {
			return mounts[i]
		}
	}
	return mounts[0] // always fall back to defaultFs mount
}

func (m *Fs) Create(name string) (afero.File, error) {
	return m.FSForPath(name).Create(name)
}

func (m *Fs) Mkdir(name string, perm os.FileMode) error {
	return m.FSForPath(name).Mkdir(name, perm)
}

func (m *Fs) MkdirAll(path string, perm os.FileMode) error {
	return m.FSForPath(path).MkdirAll(path, perm)
}

func (m *Fs) Open(name string) (afero.File, error) {
	return m.FSForPath(name).Open(name)
}

func (m *Fs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	return m.FSForPath(name).OpenFile(name, flag, perm)
}

func (m *Fs) Remove(name string) error {
	mount := m.mountForPath(name)
	if mount.path == name {
		return &os.PathError{Op: "remove", Path: name, Err: syscall.ENOSYS}
	}
	return mountedFs{mount}.Remove(name)
}

func (m *Fs) RemoveAll(path string) error {
	return m.FSForPath(path).RemoveAll(path)
}

func (m *Fs) Rename(oldname, newname string) error {
	m.mu.RLock()
	oldFs := m.FSForPath(oldname)
	oldMount := m.mountForPath(oldname)
	newFs := m.FSForPath(newname)
	newMount := m.mountForPath(newname)
	m.mu.RUnlock()

	oldInfo, err := oldFs.Stat(oldname)
	if err != nil {
		return err
	}
	if oldInfo.IsDir() {
		if oldMount.path != newMount.path {
			// TODO support dir renames across mount paths?
			log.Warnf("Attempted rename directory across mounts: %#v != %#v\nat paths: %q -> %q", oldMount, newMount, oldname, newname)
			return &os.PathError{Op: "rename", Path: oldname, Err: syscall.EXDEV}
		}
		return oldFs.Rename(oldname, newname)
	}

	oldFile, err := oldFs.Open(oldname)
	if err != nil {
		return err
	}
	defer oldFile.Close()
	newFile, err := newFs.OpenFile(newname, syscall.O_WRONLY|syscall.O_CREAT|syscall.O_TRUNC, oldInfo.Mode())
	if err != nil {
		return err
	}
	defer newFile.Close()
	_, err = io.Copy(newFile, oldFile)
	if err != nil {
		return err
	}

	oldFile.Close()
	return oldFs.Remove(oldname)
}

func (m *Fs) Stat(name string) (os.FileInfo, error) {
	return m.FSForPath(name).Stat(name)
}

func (m *Fs) Name() string {
	return "mountfs.Fs"
}

func (m *Fs) Chmod(name string, mode os.FileMode) error {
	return m.FSForPath(name).Chmod(name, mode)
}

func (m *Fs) Chtimes(name string, atime time.Time, mtime time.Time) error {
	return m.FSForPath(name).Chtimes(name, atime, mtime)
}

func (m *Fs) LstatIfPossible(name string) (os.FileInfo, bool, error) {
	fs := m.FSForPath(name)
	if lstater, ok := fs.(afero.Lstater); ok {
		return lstater.LstatIfPossible(name)
	}
	info, err := fs.Stat(name)
	return info, false, err
}
