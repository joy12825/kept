package watch

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// duplex handles one duplex folder's settled files: in the order they were
// written, each pair is the fronts then the backs of one scan. It returns
// how many files are still waiting for their other half.
func (w *Watcher) duplex(ctx context.Context, f Folder, items []pending) (int, error) {
	byTime(items)
	for len(items) >= 2 {
		front, back := items[0], items[1]
		items = items[2:]
		if err := w.mergePair(ctx, f, front, back); err != nil {
			return 0, err
		}
	}
	if len(items) == 1 {
		lone := items[0]
		if w.opt.Now().Sub(lone.info.ModTime()) < w.opt.DuplexTimeout {
			return 1, nil
		}
		w.opt.Log.Info("a duplex half waited too long; ingesting it alone", "workspace", f.WorkspaceID, "file", lone.rel)
		return 0, w.ingestFile(ctx, f, lone.path, lone.rel, filepath.Base(lone.path))
	}
	return 0, nil
}

// mergePair interleaves fronts and reversed backs into one PDF. When the
// two cannot be merged (not both PDFs, different page counts) each is
// ingested on its own.
func (w *Watcher) mergePair(ctx context.Context, f Folder, front, back pending) error {
	separately := func(why string) error {
		w.opt.Log.Warn("not merging a duplex scan", "workspace", f.WorkspaceID, "front", front.rel, "back", back.rel, "why", why)
		if err := w.ingestFile(ctx, f, front.path, front.rel, filepath.Base(front.path)); err != nil {
			return err
		}
		return w.ingestFile(ctx, f, back.path, back.rel, filepath.Base(back.path))
	}
	if !isPDF(front.path) || !isPDF(back.path) {
		return separately("not both PDFs")
	}
	nf, err := w.opt.Pages(ctx, front.path)
	if err != nil {
		return separately(err.Error())
	}
	nb, err := w.opt.Pages(ctx, back.path)
	if err != nil {
		return separately(err.Error())
	}
	if nf != nb {
		return separately(fmt.Sprintf("%d front pages, %d back pages", nf, nb))
	}
	tmp, err := os.MkdirTemp("", "kept-duplex-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	out := filepath.Join(tmp, "merged.pdf")
	if err := w.opt.Merge(ctx, front.path, back.path, out); err != nil {
		return separately(err.Error())
	}
	return w.ingestFile(ctx, f, out, front.rel, filepath.Base(front.path), front.path, back.path)
}

func isPDF(p string) bool {
	f, err := os.Open(p)
	if err != nil {
		return false
	}
	defer f.Close()
	head := make([]byte, 1024)
	n, _ := f.Read(head)
	return bytes.Contains(head[:n], []byte("%PDF-"))
}

// qpdfMerge: fronts 1..n, backs n..1, taken one page from each in turn.
func qpdfMerge(ctx context.Context, front, back, out string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "qpdf", "--empty", "--collate", "--pages", front, "1-z", back, "z-1", "--", out)
	if b, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("qpdf: %v: %s", err, strings.TrimSpace(string(b)))
	}
	return nil
}

func qpdfPages(ctx context.Context, p string) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	b, err := exec.CommandContext(ctx, "qpdf", "--show-npages", p).Output()
	if err != nil {
		return 0, fmt.Errorf("qpdf --show-npages: %w", err)
	}
	return strconv.Atoi(strings.TrimSpace(string(b)))
}
