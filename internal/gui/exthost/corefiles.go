package exthost

import (
	"context"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/confine"
)

// fileOp runs a file request off the UI thread, in one of the FileOpSlots of the scope, and replies with what it returns.
// A request that waits for a slot while its context ends fails with CoreFileWaitCanceled. The folder is the one of the
// scope, never one the request names; the functions of package confine refuse a path that leaves it.
func (s *Scope) fileOp(c *coreCall, run func(ctx context.Context, folder string) (any, error)) {
	folder := s.opts.Info.Folder
	slots := s.fileSlots
	go func() {
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		case <-c.ctx.Done():
			c.reply(nil, errors.NewErr("CoreFileWaitCanceled", c.ctx.Err()))
			return
		}
		v, err := run(c.ctx, folder)
		c.reply(v, err)
	}()
}

// coreFilesList answers emetos.files.list (confine.List). The reply is the entries of the page; More says there are more.
func (s *Scope) coreFilesList(c *coreCall) {
	var req extension.FilesListRequest
	err := decodeCore(c, &req)
	if err != nil {
		c.reply(nil, err)
		return
	}
	s.fileOp(c, func(ctx context.Context, folder string) (any, error) {
		reply, err := confine.List(ctx, folder, req.Path, confine.ListOptions{Depth: req.Depth, Offset: req.Offset, Limit: req.Limit})
		if err != nil {
			return nil, err
		}
		if reply.Entries == nil {
			reply.Entries = []extension.FileEntry{}
		}
		return reply, nil
	})
}

// coreFilesStat answers emetos.files.stat (confine.Stat).
func (s *Scope) coreFilesStat(c *coreCall) {
	var req extension.FilesPathRequest
	err := decodeCore(c, &req)
	if err != nil {
		c.reply(nil, err)
		return
	}
	s.fileOp(c, func(ctx context.Context, folder string) (any, error) {
		entry, err := confine.Stat(ctx, folder, req.Path)
		if err != nil {
			return nil, err
		}
		return entry, nil
	})
}

// coreFilesRead answers emetos.files.read (confine.Read): the text of a file of the folder, at most extension.MaxFilesReadSize.
func (s *Scope) coreFilesRead(c *coreCall) {
	var req extension.FilesPathRequest
	err := decodeCore(c, &req)
	if err != nil {
		c.reply(nil, err)
		return
	}
	s.fileOp(c, func(ctx context.Context, folder string) (any, error) {
		reply, err := confine.Read(ctx, folder, req.Path, req.MaxBytes)
		if err != nil {
			return nil, err
		}
		return reply, nil
	})
}
