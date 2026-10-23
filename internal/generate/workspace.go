package generate

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/init-co-jp/diff-tour/internal/git"
)

// Workspace is the working directory the LLM is given to read.
//
//	base/        every file before the change (at the fork point)
//	head/        every file after the change
//	change.diff  the diff from base to head
//	commits.txt  messages of the commits in the change
//	uncommitted.diff  (only when head is the working tree) the diff of just the not-yet-committed changes
//
// The LLM reads an exported copy rather than the repository itself so that its tools
// stay confined to this directory (it never needs git, and the working tree is never touched).
type Workspace struct {
	Dir string
}

func (w *Workspace) Remove() error { return os.RemoveAll(w.Dir) }

// PrepareWorkspace creates the working directory in dir. The repository is only read.
func PrepareWorkspace(repo *git.Repo, base, head, dir string) (*Workspace, error) {
	if err := os.RemoveAll(dir); err != nil {
		return nil, err
	}
	mb := repo.MergeBase(base, head)
	for name, ref := range map[string]string{"base": mb, "head": head} {
		if err := extract(repo, ref, filepath.Join(dir, name)); err != nil {
			return nil, fmt.Errorf("export %s: %w", ref, err)
		}
	}
	files := map[string]string{
		"change.diff": repo.RawDiff(base, head),
		"commits.txt": repo.CommitMessages(base, head),
	}
	if head == git.Worktree {
		// So that what is committed can be told apart from what is not yet committed.
		files["uncommitted.diff"] = repo.RawDiff("HEAD", git.Worktree)
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			return nil, err
		}
	}
	return &Workspace{Dir: dir}, nil
}

func extract(repo *git.Repo, ref, dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if ref == git.Worktree {
		return copyWorktree(repo, dir)
	}
	r, wait, err := repo.Archive(ref)
	if err != nil {
		return err
	}
	return extractArchive(r, wait, dir)
}

// extractArchive writes the regular files and directories of the tar stream r into dir, then waits for git.
// git is always waited for, even on an error, so that it never stays blocked writing to the pipe.
func extractArchive(r io.ReadCloser, wait func() error, dir string) (err error) {
	defer func() {
		r.Close()
		if werr := wait(); err == nil {
			err = werr
		}
	}()
	tr := tar.NewReader(r)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		// Reject paths that point outside the destination.
		name := filepath.Clean(h.Name)
		if filepath.IsAbs(name) || name == ".." || strings.HasPrefix(name, ".."+string(filepath.Separator)) {
			continue
		}
		target := filepath.Join(dir, name)
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
			if err != nil {
				return err
			}
			_, err = io.Copy(f, tr)
			f.Close()
			if err != nil {
				return err
			}
			// Do not export symlinks and the like (so nothing outside the working directory can be read).
		}
	}
	// tar.Reader stops at the end-of-archive marker, but git pads the archive to a whole 10 KiB record after it.
	// Read that too: a pipe on Windows holds only about 4 KiB, so git would block writing the rest and never exit.
	_, err = io.Copy(io.Discard, r)
	return err
}

// copyWorktree exports the working-tree files that may be read (tracked ones and untracked ones that are not ignored).
func copyWorktree(repo *git.Repo, dir string) error {
	for _, f := range repo.ListFiles(git.Worktree, "") {
		lines := repo.Show(git.Worktree, f)
		if lines == nil {
			continue
		}
		target := filepath.Join(dir, filepath.Clean(f))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(target, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
			return err
		}
	}
	return nil
}
