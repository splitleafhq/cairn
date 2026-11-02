package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/splitleafhq/cairn/internal/store"
)

// What init --own-branch names in git.
const (
	ownBranch    = "cairn"
	originName   = "origin"
	originBranch = originName + gitSep + ownBranch // how git names the remote's branch
	originRef    = originPrefix + ownBranch
	excludeLine  = "/" + store.DirName // the code repository's ignore line for the tracker's folder
	excludePath  = "info/exclude"      // where the code repository keeps ignore lines it does not commit
	initMessage  = commitPrefix + "init"
)

// The modes of the exclude file and its folder when init makes them.
const (
	excludePerm    fs.FileMode = 0o644
	excludeDirPerm fs.FileMode = 0o755
)

// checkoutGit is what a checkout's top folder holds: git's folder, or in a linked worktree a file that points to it.
const checkoutGit = ".git"

// refuseSecondTracker is "exists" when dir already has a .cairn, or when store.Find from dir reaches a tracker on
// its own branch, so that init never starts a tracker that would hide one already serving dir.
func refuseSecondTracker(dir string) error {
	if _, err := os.Lstat(filepath.Join(dir, store.DirName)); err == nil {
		return store.ErrExists
	}
	if s, err := store.Find(dir); err == nil && s.OwnBranch() {
		return existsError("a tracker on its own branch already serves this folder: " + s.Dir)
	}
	return nil
}

// originHasTracker reports whether dir is a checkout's top folder whose origin/cairn holds a tracker: a config.yml at
// its top. It runs git only when dir has a .git, and a git failure counts as no.
func originHasTracker(dir string) bool {
	if _, err := os.Lstat(filepath.Join(dir, checkoutGit)); err != nil {
		return false
	}
	return gitRepo{dir: dir}.holdsTracker()
}

// holdsTracker reports whether origin/cairn has a config.yml at its top.
func (g gitRepo) holdsTracker() bool {
	_, err := g.run("cat-file", "-e", originRef+":"+store.ConfigFile)
	return err == nil
}

// initOwnBranch runs init --own-branch, as the package doc describes: every check first, then the exclude line, then
// the clone or the new repository.
func (c *call) initOwnBranch() (output, error) {
	g := gitRepo{dir: c.env.Dir}
	out, err := g.run("rev-parse", "--show-prefix", "--git-dir", "--git-common-dir", "--git-path", excludePath)
	if err != nil {
		return output{}, fmt.Errorf("init --%s needs a git repository: %w", flagOwnBranch, err)
	}
	lines := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	if len(lines) != 4 {
		return output{}, fmt.Errorf("git rev-parse printed %q", out)
	}
	prefix, gitDir, commonDir, exclude := lines[0], lines[1], lines[2], lines[3]
	if prefix != "" {
		return output{}, fmt.Errorf("init --%s runs in the repository's top folder, not in %s", flagOwnBranch, prefix)
	}
	if gitDir != commonDir {
		return output{}, fmt.Errorf("init --%s runs in the main checkout, not in a linked worktree", flagOwnBranch)
	}
	attach := g.holdsTracker()
	if _, err := g.run("rev-parse", "-q", "--verify", originRef); err == nil && !attach {
		return output{}, fmt.Errorf("%s is not a tracker: it has no %s at its top", originBranch, store.ConfigFile)
	}
	url := ""
	if out, err := g.run("remote", "get-url", originName); err == nil {
		url = strings.TrimSuffix(string(out), "\n")
	}
	if !filepath.IsAbs(exclude) {
		exclude = filepath.Join(c.env.Dir, exclude)
	}
	if err := addExclude(exclude); err != nil {
		return output{}, err
	}
	dir := filepath.Join(c.env.Dir, store.DirName)
	if attach {
		if _, err := g.run("clone", "-q", "--single-branch", "--branch", ownBranch, url, dir); err != nil {
			return output{}, err
		}
		return output{initJSON{Dir: dir, Branch: ownBranch}, "attached " + dir + " to " + originBranch + "\n"}, nil
	}
	s, err := store.Init(c.env.Dir)
	if err != nil {
		return output{}, err
	}
	// A tracker left half made would block the next init, so a failed setup removes it.
	if err := setUpOwnBranch(s, url); err != nil {
		return output{}, errors.Join(err, os.RemoveAll(s.Dir))
	}
	t := gitRepo{dir: s.Dir}
	_, err = t.run("add", "-A")
	if err == nil {
		_, err = t.run("commit", "-q", "-m", initMessage)
	}
	if err != nil {
		c.warn(notCommittedPrefix + err.Error())
	}
	return output{initJSON{Dir: s.Dir, Branch: ownBranch}, "initialized " + s.Dir + " on branch " + ownBranch + "\n"}, nil
}

// setUpOwnBranch turns commit on write on in s and makes s's folder a git repository on ownBranch, with url, when it
// is not empty, as its origin and the upstream of ownBranch.
func setUpOwnBranch(s *store.Store, url string) error {
	cfg, err := s.Config()
	if err != nil {
		return err
	}
	cfg.Commit = true
	if err := s.SaveConfig(cfg); err != nil {
		return err
	}
	t := gitRepo{dir: s.Dir}
	setup := [][]string{{"init", "-q", "--initial-branch", ownBranch}}
	if url != "" {
		setup = append(setup, []string{"remote", "add", originName, url},
			[]string{"config", "branch." + ownBranch + ".remote", originName},
			[]string{"config", "branch." + ownBranch + ".merge", headsPrefix + ownBranch})
	}
	for _, args := range setup {
		if _, err := t.run(args...); err != nil {
			return err
		}
	}
	return nil
}

// addExclude adds excludeLine to the exclude file at path, making the file and its folder when they are missing,
// unless a line already says it.
func addExclude(path string) error {
	text, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	for line := range strings.SplitSeq(string(text), "\n") {
		if strings.TrimSpace(line) == excludeLine {
			return nil
		}
	}
	if len(text) > 0 && !strings.HasSuffix(string(text), "\n") {
		text = append(text, '\n')
	}
	text = append(text, excludeLine+"\n"...)
	if err := os.MkdirAll(filepath.Dir(path), excludeDirPerm); err != nil {
		return err
	}
	return store.WriteFileAtomic(path, text, excludePerm)
}
