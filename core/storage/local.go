package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Local 是本地目录驱动：对象键直接映射到 root 下的相对路径。
type Local struct{ root string }

// NewLocal 创建本地驱动，root 不存在时创建（0750）。
func NewLocal(root string) (*Local, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(abs, 0o750); err != nil {
		return nil, fmt.Errorf("storage: create %s: %w", abs, err)
	}
	return &Local{root: abs}, nil
}

// path 把键转成绝对路径，并保证不会逃出 root。
func (l *Local) path(key string) (string, error) {
	if err := ValidateKey(key); err != nil {
		return "", err
	}
	p := filepath.Join(l.root, filepath.FromSlash(key))
	if !strings.HasPrefix(p, l.root+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: %q", ErrInvalidKey, key)
	}
	return p, nil
}

// Path 返回键对应的本地绝对路径（已校验不会逃出 root）。
func (l *Local) Path(key string) (string, error) { return l.path(key) }

func (l *Local) Put(ctx context.Context, key string, r io.Reader, size int64, _ PutOptions) error {
	return l.write(ctx, key, r, size)
}

func (l *Local) PutStream(ctx context.Context, key string, r io.Reader, _ PutOptions) error {
	return l.write(ctx, key, r, -1)
}

// write 先写同目录临时文件再改名，读者不会看到写了一半的对象。
func (l *Local) write(ctx context.Context, key string, r io.Reader, size int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p, err := l.path(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".put-*")
	if err != nil {
		return err
	}
	n, err := io.Copy(tmp, r)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil && size >= 0 && n != size {
		err = fmt.Errorf("storage: expected %d bytes, got %d", size, n)
	}
	if err == nil {
		err = os.Chmod(tmp.Name(), 0o640)
	}
	if err == nil {
		err = os.Rename(tmp.Name(), p)
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
	}
	return err
}

// Adopt 把 srcPath 直接改名成对象（要求在同一文件系统）。
func (l *Local) Adopt(ctx context.Context, key, srcPath string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p, err := l.path(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		return err
	}
	return os.Rename(srcPath, p)
}

func infoOf(key string, st fs.FileInfo) *ObjectInfo {
	return &ObjectInfo{Key: key, Size: st.Size(), LastModified: st.ModTime()}
}

// Open 返回可 Seek 的文件句柄，供 http.ServeContent 使用。
func (l *Local) Open(ctx context.Context, key string) (io.ReadSeekCloser, *ObjectInfo, error) {
	p, err := l.path(key)
	if err != nil {
		return nil, nil, err
	}
	f, err := os.Open(p)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil, ErrNotFound
		}
		return nil, nil, err
	}
	st, err := f.Stat()
	if err != nil || st.IsDir() {
		_ = f.Close()
		return nil, nil, ErrNotFound
	}
	return f, infoOf(key, st), nil
}

type limitedFile struct {
	io.Reader
	io.Closer
}

func (l *Local) Get(ctx context.Context, key string, rng *Range) (*Object, error) {
	f, info, err := l.Open(ctx, key)
	if err != nil {
		return nil, err
	}
	if rng == nil {
		return &Object{Body: f, Info: *info, Length: info.Size, End: info.Size - 1}, nil
	}
	start, end, err := rng.resolve(info.Size)
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		_ = f.Close()
		return nil, err
	}
	n := end - start + 1
	return &Object{Body: limitedFile{io.LimitReader(f, n), f}, Info: *info, Length: n, Partial: true, Start: start, End: end}, nil
}

func (l *Local) Delete(ctx context.Context, key string) error {
	p, err := l.path(key)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

func (l *Local) Exists(ctx context.Context, key string) (bool, error) {
	if _, err := l.Stat(ctx, key); err != nil {
		if errors.Is(err, ErrNotFound) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (l *Local) Stat(ctx context.Context, key string) (*ObjectInfo, error) {
	p, err := l.path(key)
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(p)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if st.IsDir() {
		return nil, ErrNotFound
	}
	return infoOf(key, st), nil
}

func (l *Local) PresignGet(context.Context, string, time.Duration, PresignOptions) (string, error) {
	return "", ErrNotSupported
}

var (
	_ Storage = (*Local)(nil)
	_ Opener  = (*Local)(nil)
	_ Adopter = (*Local)(nil)
)
