package app

import (
	"context"
	"os"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/fileio"
	plistpkg "github.com/rm4n0s/emetos/internal/core/plist"
)

func readAgain(ctx context.Context, path string) error {
	_, err := fileio.Read(ctx, path, fileio.ReadOptions{})
	if err != nil {
		return errors.NewErr("ReadFailed", err)
	}
	return nil
}

func inInit(ctx context.Context, path string) error {
	if _, err := fileio.Write(ctx, path, "x", fileio.WriteOptions{}); err != nil {
		return errors.NewErr("WriteFailed", err).SetMetadata("path", path)
	}
	return nil
}

func aliased(data []byte) error {
	root, err := plistpkg.Parse(data)
	if err != nil {
		return errors.NewErrf("PlistFailed", "parse: %w", err)
	}
	_ = root
	return nil
}

func reassigned(ctx context.Context, path string) error {
	err := os.Remove(path)
	if err != nil {
		return errors.NewErr("RemoveFailed", err)
	}
	_, err = fileio.Read(ctx, path, fileio.ReadOptions{})
	switch {
	case err != nil:
		return errors.NewErr("ReadFailed", err)
	}
	return nil
}

// Standard library errors are wrapped once, where they first appear.
func stdlib(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return errors.NewErr("OpenFailed", err)
	}
	return f.Close()
}

// The inner err belongs to the if: the outer one comes from the standard library.
func shadowed(ctx context.Context, path string) error {
	err := os.Remove(path)
	if _, err := fileio.Read(ctx, path, fileio.ReadOptions{}); err != nil {
		return err
	}
	if err != nil {
		return errors.NewErr("RemoveFailed", err)
	}
	return nil
}

// An error that is tagged already is passed on; only the others get the tag.
func guarded(ctx context.Context, path string) error {
	_, err := fileio.Read(ctx, path, fileio.ReadOptions{})
	if err != nil {
		if _, ours := errors.FromError(err); !ours {
			err = errors.NewErr("ReadFailed", err)
		}
		return err
	}
	return nil
}

// Methods and functions of the package are not seen: only review catches them.
type disk struct{}

func (disk) stat() error { return nil }

func method(d disk) error {
	err := d.stat()
	if err != nil {
		return errors.NewErr("StatFailed", err)
	}
	return nil
}
