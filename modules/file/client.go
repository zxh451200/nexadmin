package file

// 宿主侧进程内 API：其它内置模块/插件按 sys_file 主键复用已上传文件的内容
// （如知识库从文件库选一篇 PDF 直接切分向量化），不走 HTTP 自调用，也不互扒
// 对方的存储实现。注册点在 module.Register 装配完成后（SetDefault）。

import (
	"context"
	"errors"
	"io"

	"nexadmin/pkg/idgen"
)

// ErrInactive / ErrNoContent 是 OpenByID 的两个专属失败语义：模块未装配、
// 文件只登记了外部地址（StoragePath 为空）没有本站可读内容。
var (
	ErrInactive  = errors.New("file: module inactive")
	ErrNoContent = errors.New("file: external url has no stored content")
)

var defaultSvc = make(chan *Service, 1)

// SetDefault 由模块装配层调用，注册进程级默认文件服务。
func SetDefault(s *Service) {
	select {
	case <-defaultSvc: // 已有则替换
	default:
	}
	defaultSvc <- s
}

func current() *Service {
	select {
	case s := <-defaultSvc:
		defaultSvc <- s
		return s
	default:
		return nil
	}
}

// FileRef 是按主键打开的库文件：元信息与只读内容流。调用方负责 Close。
type FileRef struct {
	Name string
	MIME string
	Size int64
	Body io.ReadCloser
}

// OpenByID 按 sys_file 主键取文件元信息并打开内容流。租户隔离随 ctx 走
// （TenantModel 自动过滤），拿不到别的租户的文件。外部地址型文件
// （StoragePath 为空，只登记了 URL）没有可读内容，返回 ErrNoContent。
func OpenByID(ctx context.Context, id idgen.ID) (*FileRef, error) {
	s := current()
	if s == nil {
		return nil, ErrInactive
	}
	var f File
	if err := s.db.WithContext(ctx).First(&f, "id = ?", int64(id)).Error; err != nil {
		return nil, err
	}
	if f.StoragePath == "" {
		return nil, ErrNoContent
	}
	obj, err := s.store.blobs.Get(ctx, f.StoragePath, nil)
	if err != nil {
		return nil, err
	}
	return &FileRef{Name: f.Name, MIME: f.MimeType, Size: f.Size, Body: obj.Body}, nil
}
