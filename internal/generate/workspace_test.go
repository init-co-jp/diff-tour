package generate

import (
	"archive/tar"
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/init-co-jp/diff-tour/internal/git"
)

func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestPrepareWorkspace(t *testing.T) {
	repoDir := t.TempDir()
	run(t, repoDir, "init", "-q", "-b", "main")
	write(t, filepath.Join(repoDir, "src/a.txt"), "fork point\n")
	run(t, repoDir, "add", "-A")
	run(t, repoDir, "commit", "-q", "-m", "base")
	run(t, repoDir, "switch", "-q", "-c", "feature")
	write(t, filepath.Join(repoDir, "src/a.txt"), "feature\n")
	run(t, repoDir, "commit", "-q", "-am", "change a")
	run(t, repoDir, "switch", "-q", "main")
	write(t, filepath.Join(repoDir, "src/a.txt"), "main moved on\n")
	run(t, repoDir, "commit", "-q", "-am", "main")
	before := run(t, repoDir, "status", "--porcelain") + run(t, repoDir, "log", "--all", "--oneline")

	ws, err := PrepareWorkspace(git.Open(repoDir), "main", "feature", filepath.Join(t.TempDir(), "ws"))
	if err != nil {
		t.Fatal(err)
	}
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join(ws.Dir, name))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	if got := read("base/src/a.txt"); got != "fork point\n" {
		t.Errorf("base = %q (should be the merge base, not the tip of main)", got)
	}
	if got := read("head/src/a.txt"); got != "feature\n" {
		t.Errorf("head = %q", got)
	}
	if got := read("commits.txt"); !strings.Contains(got, "change a") || strings.Contains(got, "main") {
		t.Errorf("commits = %q", got)
	}
	if got := read("change.diff"); !strings.Contains(got, "+feature") {
		t.Errorf("diff = %q", got)
	}
	if after := run(t, repoDir, "status", "--porcelain") + run(t, repoDir, "log", "--all", "--oneline"); after != before {
		t.Errorf("repository changed:\n%s\n---\n%s", before, after)
	}
}

// archiveFrom serves a tar the way git archive does: padded past the end-of-archive marker,
// through a pipe with no buffer, so the writer exits only once everything has been read
// (on Windows the pipe from git holds only about 4 KiB).
func archiveFrom(t *testing.T, files map[string]string) (io.ReadCloser, func() error) {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for name, content := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		tw.Write([]byte(content))
	}
	tw.Close()
	buf.Write(make([]byte, 10240))
	r, w := io.Pipe()
	done := make(chan error, 1)
	go func() {
		_, err := w.Write(buf.Bytes())
		w.Close()
		done <- err
	}()
	wait := func() error {
		select {
		case err := <-done:
			return err
		case <-time.After(5 * time.Second):
			return errors.New("the writer never finished (the archive was not read to the end)")
		}
	}
	return r, wait
}

func TestExtractArchiveReadsToTheEnd(t *testing.T) {
	r, wait := archiveFrom(t, map[string]string{"src/a.txt": "a\n"})
	dir := t.TempDir()
	if err := extractArchive(r, wait, dir); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "src/a.txt")); err != nil || string(b) != "a\n" {
		t.Errorf("a.txt = %q, %v", b, err)
	}
}

func TestExtractArchiveWaitsOnError(t *testing.T) {
	r, wait := archiveFrom(t, map[string]string{"src/a.txt": "a\n"})
	dir := t.TempDir()
	// A file where the directory should go makes MkdirAll fail.
	write(t, filepath.Join(dir, "src"), "")
	waited := false
	err := extractArchive(r, func() error { waited = true; return wait() }, dir)
	if err == nil {
		t.Fatal("no error")
	}
	if !waited {
		t.Error("returned without waiting for git")
	}
}
