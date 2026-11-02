package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os/exec"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/splitleafhq/cairn/internal/id"
	"github.com/splitleafhq/cairn/internal/model"
	"github.com/splitleafhq/cairn/internal/store"
)

// branchVersion is an issue as one branch's last commit has it. branch is "" for the checkout's own version.
type branchVersion struct {
	branch   string
	issue    model.Issue
	comments []model.Comment // every comment, retracted ones included
}

// What --branches asks git for, and how git answers.
const (
	gitCommand   = "git"
	headsPrefix  = "refs/heads/"
	originPrefix = "refs/remotes/origin/"
	originHEAD   = originPrefix + "HEAD"
	activeWindow = 14 * 24 * time.Hour // how recent an active branch's last commit is
	gitSep       = "/"                 // the path separator in git, on every system
	formatNUL    = "%00"               // a NUL in a for-each-ref format
	nul          = "\x00"              // what formatNUL prints, and what ends each path with -z
	currentMark  = "*"                 // %(HEAD) for the branch checked out here
	noMergeBase  = 1                   // git merge-base's exit status when two commits share no history
)

// fallbackDefaults are the default branch's names when origin/HEAD names no local branch, in order.
var fallbackDefaults = []string{"main", "master"}

// branchFields are the for-each-ref fields localBranches reads: the ref, its commit, the commit's committer date in
// Unix seconds, and currentMark or a space.
var branchFields = []string{"%(refname)", "%(objectname)", "%(committerdate:unix)", "%(HEAD)"}

// branchIssues returns the issues that active branches changed, as each branch's last commit has them: branches in
// name order, and each branch's issues in store order. When issueID is not "", it reads that issue alone. A file that
// cannot be read or does not parse leaves its issue out, with a warning that names the branch and the issue. Failing
// to run git, or finding no default branch, is an error. A tracker on its own branch has no other branch to read, so
// it returns none and runs no git.
//
// The default branch is the first of origin/HEAD's branch and fallbackDefaults that is a local branch. An active
// branch is any other local branch, in any worktree, that:
//   - is not checked out here;
//   - has a last commit less than activeWindow before now;
//   - shares history with the default branch, and is not merged into it;
//   - changed files in the issues folder since it left the default branch.
//
// git runs in the tracker's folder and is given commits by id, so a tag named like a branch changes nothing. Paths
// are relative to the tracker's folder, which can sit below the repository's top folder: diff-tree --relative and
// ls-tree print them so, and git reads a path after "<commit>:./" so. Nothing compares absolute paths, which on macOS
// can differ by a symlink.
func (c *call) branchIssues(s *store.Store, issueID string) ([]branchVersion, error) {
	if s.OwnBranch() {
		return nil, nil
	}
	folder := store.IssuesDir + gitSep
	if issueID != "" {
		if !id.Valid(issueID) {
			return nil, nil
		}
		folder += issueID + gitSep
	}
	g := gitRepo{dir: s.Dir}
	branches, err := g.localBranches()
	if err != nil {
		return nil, err
	}
	def, err := g.defaultBranch(branches)
	if err != nil {
		return nil, err
	}
	now := c.env.stamp()
	var versions []branchVersion
	for _, b := range branches {
		if b.name == def.name || b.current || now.Sub(b.date) >= activeWindow {
			continue
		}
		found, err := c.readBranch(g, def, b, folder)
		if err != nil {
			return nil, err
		}
		versions = append(versions, found...)
	}
	return versions, nil
}

// readBranch reads the issues in folder that b changed since it left def, in store order. It reads none when b
// shares no history with def or is merged into it.
func (c *call) readBranch(g gitRepo, def, b localBranch, folder string) ([]branchVersion, error) {
	base, ok, err := g.mergeBase(def.commit, b.commit)
	if err != nil || !ok || base == b.commit {
		return nil, err
	}
	changed, err := g.paths("diff-tree", "-r", "-z", "--name-only", "--relative", base, b.commit, "--", folder)
	if err != nil {
		return nil, err
	}
	issueIDs := map[string]bool{}
	for _, p := range changed {
		if issueID, _, ok := issuePath(p); ok {
			issueIDs[issueID] = true
		}
	}
	if len(issueIDs) == 0 {
		return nil, nil
	}
	files, err := g.paths("ls-tree", "-r", "-z", "--name-only", b.commit, "--", folder)
	if err != nil {
		return nil, err
	}
	names := map[string][]string{}
	for _, p := range files {
		if issueID, name, ok := issuePath(p); ok && !strings.Contains(name, gitSep) {
			names[issueID] = append(names[issueID], name)
		}
	}
	var found []branchVersion
	for _, issueID := range slices.Sorted(maps.Keys(issueIDs)) {
		i, comments, err := store.ReadIssue(issueID, g.folder(b.commit, issueID, names[issueID]))
		switch {
		case errors.Is(err, store.ErrNotFound): // the branch removed it, or it is not an issue's folder
		case err != nil:
			c.warn(fmt.Sprintf("branch %s: issue %s: %v", b.name, issueID, err))
		default:
			found = append(found, branchVersion{branch: b.name, issue: i, comments: comments})
		}
	}
	slices.SortFunc(found, func(x, y branchVersion) int { return store.CompareIssues(x.issue, y.issue) })
	return found, nil
}

// issuePath splits p, a path in the issues folder relative to the tracker's folder, into the name of the issue
// folder it is in and the path inside that folder. ok is false for a path that is not inside an issue folder.
func issuePath(p string) (issueID, inside string, ok bool) {
	if p, ok = strings.CutPrefix(p, store.IssuesDir+gitSep); !ok {
		return "", "", false
	}
	return strings.Cut(p, gitSep)
}

// gitRepo runs git in one folder.
type gitRepo struct {
	dir string
}

// localBranch is a local branch as for-each-ref lists it.
type localBranch struct {
	name    string    // without headsPrefix
	commit  string    // its last commit's id
	date    time.Time // its last commit's committer date
	current bool      // checked out here
}

// localBranches lists the local branches in name order.
func (g gitRepo) localBranches() ([]localBranch, error) {
	format := "--format=" + strings.Join(branchFields, formatNUL)
	out, err := g.run("for-each-ref", "--sort=refname", format, headsPrefix)
	if err != nil {
		return nil, err
	}
	badLine := func(line string) error { return fmt.Errorf("git for-each-ref: unexpected line %q", line) }
	var branches []localBranch
	for _, line := range records(out, "\n") {
		fields := strings.Split(line, nul)
		if len(fields) != len(branchFields) {
			return nil, badLine(line)
		}
		ref, commit, date, head := fields[0], fields[1], fields[2], fields[3]
		name, ok := strings.CutPrefix(ref, headsPrefix)
		unix, err := strconv.ParseInt(date, 10, 64)
		if !ok || err != nil {
			return nil, badLine(line)
		}
		branches = append(branches, localBranch{name, commit, time.Unix(unix, 0), head == currentMark})
	}
	return branches, nil
}

// defaultBranch is the first of origin/HEAD's branch and fallbackDefaults that is in branches.
func (g gitRepo) defaultBranch(branches []localBranch) (localBranch, error) {
	out, err := g.run("for-each-ref", "--format=%(symref)", originHEAD)
	if err != nil {
		return localBranch{}, err
	}
	var names []string
	if origin, ok := strings.CutPrefix(strings.TrimSpace(string(out)), originPrefix); ok {
		names = append(names, origin)
	}
	for _, name := range append(names, fallbackDefaults...) {
		if n := slices.IndexFunc(branches, func(b localBranch) bool { return b.name == name }); n >= 0 {
			return branches[n], nil
		}
	}
	return localBranch{}, fmt.Errorf("no default branch: %s names no local branch, and there is no %s", originHEAD,
		strings.Join(fallbackDefaults, " or "))
}

// mergeBase returns the commit where b's history left a's, or false when they share no history.
func (g gitRepo) mergeBase(a, b string) (string, bool, error) {
	out, err := g.run("merge-base", a, b)
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == noMergeBase {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return strings.TrimSpace(string(out)), true, nil
}

// paths runs a git command that prints paths with -z, and returns them.
func (g gitRepo) paths(args ...string) ([]string, error) {
	out, err := g.run(args...)
	return records(out, nul), err
}

// folder is the issue folder issueID in commit, holding the files names.
func (g gitRepo) folder(commit, issueID string, names []string) store.Folder {
	dir := path.Join(store.IssuesDir, issueID)
	return store.Folder{Path: path.Join(store.DirName, dir), Names: names, Read: func(name string) ([]byte, error) {
		if !slices.Contains(names, name) {
			return nil, fs.ErrNotExist
		}
		return g.run("cat-file", "blob", blob(commit, path.Join(dir, name)))
	}}
}

// blob names the file at p in commit, with p relative to the folder git runs in.
func blob(commit, p string) string {
	return commit + ":./" + p
}

// pathspecEnv names the variables that change how git reads every pathspec. run leaves them out, so a pathspec cairn
// gives, such as commit on write's ":(literal)" path, means the same in every environment.
var pathspecEnv = []string{"GIT_LITERAL_PATHSPECS", "GIT_GLOB_PATHSPECS", "GIT_NOGLOB_PATHSPECS", "GIT_ICASE_PATHSPECS"}

// run runs git with args and returns what it prints on stdout. A failure is an error that holds git's stderr on one
// line, and unwraps to the *exec.ExitError when git ran.
func (g gitRepo) run(args ...string) ([]byte, error) {
	cmd := exec.Command(gitCommand, args...)
	cmd.Dir = g.dir
	cmd.Env = slices.DeleteFunc(cmd.Environ(), func(kv string) bool {
		k, _, _ := strings.Cut(kv, "=")
		return slices.Contains(pathspecEnv, k)
	})
	out, err := cmd.Output()
	if err == nil {
		return out, nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		if msg := oneLine(exit.Stderr); msg != "" {
			err = fmt.Errorf("%w: %s", err, msg)
		}
	}
	return nil, fmt.Errorf("git %s: %w", args[0], err)
}

// oneLine is text as one line: its lines that are not blank, trimmed and joined by "; ".
func oneLine(text []byte) string {
	var lines []string
	for l := range strings.Lines(string(text)) {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	return strings.Join(lines, "; ")
}

// records splits out into the records that each end in end.
func records(out []byte, end string) []string {
	s := strings.TrimSuffix(string(out), end)
	if s == "" {
		return nil
	}
	return strings.Split(s, end)
}
