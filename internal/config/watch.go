package config

import (
	"bytes"
	"context"
	"os"
	"time"
)

// Change is a change of the configuration file seen by Watch.
type Change struct {
	Data    []byte // new contents; nil when Removed
	Removed bool
	Err     error // the file exists but could not be read
}

// stamp identifies a version of the file without reading it.
type stamp struct {
	exists  bool
	size    int64
	modTime time.Time
	ino     uint64
}

func statFile(path string) stamp {
	fi, err := os.Stat(path)
	if err != nil {
		return stamp{}
	}
	return stamp{exists: true, size: fi.Size(), modTime: fi.ModTime(), ino: inode(fi)}
}

// Watch reports changes of the file at path until ctx ends (CFG-06). It looks
// at the file every interval; that costs one stat call and no measurable CPU,
// and it works with every editor, including those that save by writing a new
// file and renaming it over the old one. After a change it waits until the
// file has stayed the same for settle, so a half-written file is not read.
// Saving without changing the contents is not reported.
//
// current is the contents already in use (nil if there is no file).
func Watch(ctx context.Context, path string, current []byte, interval, settle time.Duration, out chan<- Change) {
	last := statFile(path)
	lastData := current
	lastRemoved := current == nil && !last.exists
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		now := statFile(path)
		if now == last {
			continue
		}
		// Wait until the editor is done.
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(settle):
			}
			again := statFile(path)
			if again == now {
				break
			}
			now = again
		}
		last = now

		var ch Change
		switch {
		case !now.exists:
			if lastRemoved {
				continue
			}
			lastRemoved, lastData = true, nil
			ch = Change{Removed: true}
		default:
			data, err := os.ReadFile(path)
			if err != nil {
				if os.IsNotExist(err) {
					continue // gone again; the next tick sees it
				}
				ch = Change{Err: err}
				break
			}
			if !lastRemoved && bytes.Equal(data, lastData) {
				continue
			}
			lastRemoved, lastData = false, data
			ch = Change{Data: data}
		}
		select {
		case out <- ch:
		case <-ctx.Done():
			return
		}
	}
}
