// Package cli runs cairn's commands. It never prompts and never opens an editor.
//
// Arguments: flags start with "--" and take "--name value" or "--name=value"; --json, --retracted, --branches,
// --own-branch, --closed, --archived, --owed and --all take no value. Flags and positional arguments can come in any
// order, and "--" ends the flags. A flag that takes a value takes the next argument even when it starts with "-", so
// "--tag -t" works, but not one that starts with "--": that is a missing value, and "--name=--value" passes such a
// value. --tag can be repeated; for any other flag the last one wins. No arguments at all is a usage error. help with
// any arguments, and "--help" or "-h" anywhere before "--" (even as a flag's value, and after any command), run help.
// "--version" means version. search --meaning, which a later step builds, is a usage error until then. So are
// --retracted anywhere but show, --branches anywhere but ls and show, --own-branch anywhere but init, --closed
// and --owed anywhere but ls, --archived, --limit and --all anywhere but ls and search, and --html, which takes a
// value, anywhere but board.
//
// Tracker: "-C <dir>" as the first two arguments runs the command as if Env.Dir were dir, after Env.Dir when it is
// relative, with its links resolved before any ".." in it; a dir that is missing or not a folder is an "error", and a
// missing dir, or one that starts with "--", is a usage error. Help still wins, as above. A "-C" anywhere else before
// "--", a flag's value included, is a usage error. Every command but init, help and version uses the
// tracker that store.Find reaches from Env.Dir, else, without -C, the global tracker: store.Global of the user config
// file, store.UserConfigFile in $XDG_CONFIG_HOME when it is an absolute path, else in $HOME/.config. When neither
// names an absolute path, there is no global tracker.
//
// Output: --json anywhere before "--" turns on JSON, even when the other arguments are wrong. With --json, success
// prints {"ok": true, "data": …} and failure prints
// {"ok": false, "error": {"code": "…", "message": "…"}} on stdout, plus the message on one line of stderr. Without
// --json, success prints text on stdout and failure prints "cairn: <message>" on one line of stderr and nothing on
// stdout. A command that succeeds with warnings still exits 0, and prints each warning in both modes as one line
// "cairn: warning: <message>" on stderr. With --json, its envelope also has "warnings": [<message>, …], in the same
// order, and without warnings it has no "warnings" key. A command that fails prints no warnings. JSON writes <, > and &
// as they are, not as \u003c, \u003e and \u0026. ls and search also have "total" in their envelope, after "data", as
// --limit below says; no other command's envelope has it.
//
// Exit codes and error codes:
//   - 0: ok.
//   - 1: "no_tracker" (no .cairn here or above, and no global tracker), "exists" (init where .cairn exists or another tracker would be
//     hidden, or milestone new for an id that has a file), "error" (anything else).
//   - 2: "usage": an unknown command or flag, a missing or extra argument, a missing value, or a value in the wrong
//     form, including a bad date, a status not in the config, a tag whose group is not in the config, and a write
//     with no actor.
//   - 4: "not_found": no issue, comment or milestone with that id, including an id that is not valid. The message names
//     the id that was not found.
//
// Writes (new, edit, rm, comment, retract, purge, milestone new, milestone edit) need an actor: --as, or else the
// CAIRN_ACTOR environment variable. They also take --persona and --model. new records them on the issue as by, persona
// and model, once: no later write changes them, and an issue without them keeps "". comment records them the same way
// on the comment, and retract records the actor as retracted_by; the other writes check the actor but store it nowhere.
//
// Commit on write: when the config's commit is true, a write that succeeds commits what it changed, as it is on disk:
// its issue's folder, issues/<id>, or for milestone new and milestone edit the milestone's file, milestones/<id>.md.
// git runs in the tracker's folder, first "git add -A -- <path>", then "git commit -m <message> -- <path>", so nothing
// else is staged or committed, and an earlier change to the same issue that was never committed goes in too. The
// message is "chore(cairn): <command> <id> (by <actor>)", where command is new, edit, rm, comment, retract, purge,
// milestone new or milestone edit, and id is the issue's or the milestone's. git's own config names the author, and
// the repository's hooks run. Each git step runs once, so two writes at the same moment can collide on git's locks.
// When git fails (no git, no repository, another writer's lock, a hook), or config.yml cannot be read after a write
// that did not need it, the write stands and the command still succeeds, with the warning
// "not committed: <the error>". But when git status then shows nothing left to commit at the path, another writer
// committed the change first, and there is no warning, unless git ignores the path itself: git status shows nothing
// there and git add refuses it, so it still warns. Ignored files inside the path do not count. A write that fails
// commits nothing, and init commits only as init --own-branch describes.
//
// Commands:
//   - init: makes .cairn in Env.Dir (it does not look upward for a place to put it). It is "exists", and changes
//     nothing, when Env.Dir already has a .cairn, or when store.Find from Env.Dir reaches a tracker on its own branch,
//     as a linked worktree reaches its main checkout's, with a message that names that tracker's folder; with or
//     without --own-branch. Without it, when Env.Dir has a .git, init runs git once, and is "exists" too when
//     origin/cairn holds a tracker (a config.yml at its top), with a message that names cairn init --own-branch.
//     Text: "initialized <the .cairn path>\n". Data: {"dir": the .cairn path, "branch": ""}.
//   - init --own-branch: keeps the tracker on the branch cairn, in a git repository of its own at .cairn. After
//     those two checks, Env.Dir must be the top folder of a main checkout, not a subfolder or a linked worktree, and an
//     origin/cairn, when there is one, must hold a tracker; anything else is an "error", and changes nothing. Then it
//     adds the line "/.cairn" to the repository's info/exclude, unless a line already says it. When origin/cairn holds
//     a tracker, it attaches it, git clone --single-branch --branch cairn <origin's URL> .cairn, and its text is
//     "attached <the .cairn path> to origin/cairn\n". Otherwise it makes it: init's files with commit: true in
//     config.yml, git init on the branch cairn, with origin's URL as the remote origin and origin/cairn as the
//     branch's upstream when the code repository has an origin, and one commit of every file, "chore(cairn): init".
//     When a step before that commit fails, it removes .cairn and is an "error", so init can run again; the exclude
//     line stays. When that commit fails, the tracker stands, uncommitted, with the warning "not committed: <the error>". Its
//     text is then "initialized <the .cairn path> on branch cairn\n". Data either way: {"dir": the .cairn path,
//     "branch": "cairn"}. It never pushes.
//   - new "title" [--body text] [--tag t]... [--milestone m] [--due YYYY-MM-DD]: adds an issue with the
//     config's first status, created and updated set to Env.Now in UTC truncated to the millisecond, and a new id.
//     The title must not be blank. A non-empty body that does not end in "\n" gets one. Repeated tags are kept once.
//     A config with no statuses is an "error". Text: the id and "\n". Data: the issue.
//   - edit <id> [--title t] [--body text] [--status s] [--tag +t] [--tag -t] [--milestone m] [--due d]: changes
//     only what is given, and at least one change is required. "+t" or "t" adds a tag (no duplicates), "-t" removes
//     it; a removed tag only has to be non-empty, so a tag whose group left the config can still go. An empty
//     --milestone or --due clears it. updated becomes Env.Now as in new. Text: the id and "\n". Data: the issue.
//   - rm <id>: deletes the issue folder, comments included. Text: "removed <id>\n". Data: {"id": id}.
//   - show <id> [--retracted]: reads any issue, closed and archived ones included. Text: the issue for a person to
//     read, with its by, persona and model when set, then its active comments in order, each with its id, kind, by,
//     persona and model when set, at, source when set, and body. --retracted adds the retracted comments in their
//     places, each marked "retracted" with its retracted_by, retracted_at, reason and disproved_by. Data: the issue
//     plus "comments", its active comments in order (all of them with --retracted), a list never null.
//   - ls [--status s] [--closed] [--archived] [--owed] [--tag t]... [--milestone m] [--due-before YYYY-MM-DD]: lists
//     issues in store order. Every filter given must match; --due-before keeps issues with a due date before d, and
//     --owed keeps issues whose latest active decision comment has no active result comment with a later at. Without
//     --status, ls leaves out closed issues unless --closed is given, and archived issues unless --archived is
//     given, as the config's closed and archived name them (model.Config.IsClosed and IsArchived); with --status s,
//     it keeps the issues whose status is s, whatever s is. Text: one "<id>\t<status>\t<title>\n" line per issue.
//     Data: a list of issues without "body" (an empty list, never null).
//   - --branches, on ls and show, adds what active branches committed, as branchIssues reads it. Without it, commit on
//     write and init, Cairn never runs git. With it, failing to read git (no git, no repository, no default branch) is
//     an "error", and a branch file that cannot be read or does not parse is a warning that names the branch and the
//     issue. On a tracker on its own branch it adds nothing and runs no git.
//   - ls --branches: the checkout's rows, then the branch versions, all through the same filters, with the
//     checkout's config. Text: a branch version's line is "<id>\t<status>\t<title>\t<branch>\n". Data: every item
//     also has "branch" ("" for the checkout).
//   - show <id> --branches: the checkout's version of the issue, then each branch version of it, by branch;
//     not_found when there is none. It reads only that issue from each branch, so its warnings are about that issue.
//     --retracted applies to each version. Text: each version as show prints it, a branch version after a line
//     "=== branch <name> ===", with a blank line before every version but the first. Data: a list of show's data,
//     each with "branch" ("" for the checkout), a list even when there is one version.
//   - comment <id> --kind evidence|decision|result [--source s] "text": adds a comment with a new id, by, persona and
//     model from the actor flags, and at set to Env.Now as in new. The text must not be blank, and gets a trailing
//     "\n" as a body does. evidence needs a --source that is not blank; any other kind, github included, is a usage
//     error. A comment never changes issue.md. When the text's words.Overlap with a retracted comment on the same
//     issue reaches the config's retracted_match, the comment is still added, with one warning per such comment in
//     comment order: "matches retracted comment <comment id> (reason <reason as %q>)". Text: the new comment id and
//     "\n". Data: {"comment": the comment, "retracted_matches": [{"comment": id, "reason": reason, "score":
//     overlap}, …]}, retracted_matches an empty list when there are none.
//   - retract <id> <comment> --reason "…" [--disproved-by <comment>]: keeps the comment and sets retracted_by to the
//     actor, retracted_at to Env.Now as in new, retracted_reason, and disproved_by. The reason must not be blank.
//     --disproved-by names another comment on the same issue: one that does not exist is not_found, and the comment
//     itself is a usage error. A comment already retracted is an "error" and stays as it was. Text: "retracted
//     <comment>\n". Data: the comment.
//   - purge <id> <comment>: deletes the comment's file, retracted or not. Text: "purged <comment>\n". Data:
//     {"id": the issue id, "comment": the comment id}.
//   - tags: every tag on any issue, with the number of issues that have it (an issue counts once), sorted by tag in
//     byte order. Text: one "<tag>\t<count>\n" line per tag. Data: a list of {"tag", "count"} (an empty list, never
//     null).
//   - milestones: every milestone with a file in milestones/ and every milestone an issue names, with the number of
//     issues that name it. A milestone without a file has "" for title, status and due. Sorted by due, those without
//     one last, then by id. Text: one "<id>\t<due>\t<issues>\t<title>\n" line each. Data: a list of {"id", "title",
//     "status", "due", "issues"} (an empty list, never null).
//   - milestone new <id> "title" [--due YYYY-MM-DD]: writes milestones/<id>.md with the config's first status. The id
//     must not be empty, contain "/" or start with "." (a usage error). An id that already has a file, or that differs
//     only in case from one that does (the two name one file on a case-insensitive file system), is "exists", and a
//     file that does not parse is an "error"; either way the file is left as it was. The title must not be blank.
//     Text: the id and "\n". Data: the milestone.
//   - milestone edit <id> [--title t] [--status s] [--due YYYY-MM-DD]: changes only what is given, and at least one
//     change is required. The title must not be blank, the status must be one of the config's, and an empty --due
//     clears it. The file's other keys and its body are kept. A milestone without a file is not_found, and a file
//     that does not parse is an "error". Text: the id and "\n". Data: the milestone.
//   - milestone with no subcommand, or any other subcommand, is a usage error. The subcommand is the argument right
//     after milestone.
//   - search <words>... [--archived]: the positional arguments joined by spaces are the query, and its words are
//     words.Split of it; a query with no words is a usage error. Archived issues, as ls reads them, are left out
//     unless --archived is given; closed ones are searched. An issue's matches are the query words found among the
//     words of its title, its body, and the bodies and sources of its active comments; tags are not searched. Issues
//     with at least one match are listed by matches, most first, then in store order. Text: one
//     "<id>\t<matches>\t<title>\n" line each. Data: a list of issues without "body", each with "matches" added (an
//     empty list, never null, when nothing matches).
//   - board [--html <file>]: the issues in a column for each status, as internal/board lays them out: the config's
//     statuses in its order, empty ones too, then any other status an issue has, in byte order. A column holds its
//     issues with the last update first, and is collapsed when the config's closed or archived names its status.
//     It takes no positional argument and only reads: it needs no actor and commits nothing. Without --html, text is
//     board.Text: a header "<status> (<count>)" per column, with ", collapsed" after the count for a collapsed one,
//     and a tab, then the id, last update, tags joined by ", " and title, separated by tabs, for each issue of a
//     column that is not collapsed; blank lines separate columns. Data: {"columns": [{"status", "collapsed",
//     "count", "issues"}, …]}, where issues are rows as ls gives them and are [] for a collapsed column, whatever its
//     count. With --html, it writes the page board.HTML makes, every column's issues included, to the file, replacing
//     it through store.WriteFileAtomic with mode 0644: a relative name is relative to Env.Dir, the folder must
//     exist, an empty name is a usage error and a folder is an "error". The page is one fixed template filled with data; it loads and runs
//     nothing. Its title names the tracker by the name of the folder that holds the .cairn, and it says when Env.Now
//     made it. Text: "wrote <the file>\n". Data: {"file": its path, made absolute as above, "issues": how many
//     issues the page holds}. Neither has "total".
//   - --limit N and --all, on ls and search, cap the rows they print. Without either, they print at most
//     defaultLimit (30) rows; with --limit, at most N, which must be a whole number of 1 or more as strconv.Atoi
//     reads it; with --all, every row. Any other N, and --limit with --all, is a usage error. The cut comes after the
//     filters and the sort: ls keeps its last N rows, which are the newest issues, store order being oldest first,
//     and with --branches the branch versions, which come last; search keeps its first N, the most matches. Text and
//     data hold the same rows. When rows are dropped, the command warns "<shown> of <total> shown; --limit N or --all
//     for more". With --json, the envelope of ls and search always has "total": the number of rows before the cut, 0
//     when there are none.
//   - version.
//   - help prints a line that says what --json prints, then commandUsage, one usage line per command, then
//     agentRules, the rules in docs/spec.md. Data: {"usage": commandUsage, "rules": agentRules}.
//
// A tag is non-empty with no space, ",", "[" or "]". When it has a ":", the part before the first ":" must be one of
// the config's tag_groups. A milestone, as an id or as what an issue names, has no "/" and does not start with ".";
// an issue's milestone can also be "", for none.
//
// A milestone in data is {"id", "title", "status", "due"}, every key always present ("" when due is not set).
//
// An issue in data is {"id", "title", "status", "tags", "milestone", "due", "by", "persona", "model", "created",
// "updated", "github", "body"}, every key always present: tags is a list (never null), times are strings in
// model.TimeLayout (RFC 3339 UTC with three fraction digits, in text output too), github is a number (0 when not
// synced), and empty text is "". Only show adds "comments".
//
// A comment in data is {"id", "kind", "by", "persona", "model", "at", "source", "body", "retracted"}, every key always
// present: retracted is null for an active comment, and otherwise {"by", "at", "reason", "disproved_by"}, with ""
// for no disproved_by.
package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/splitleafhq/cairn/internal/store"
)

// Env is everything a command touches outside its arguments. A nil Now means time.Now, a nil Rand means
// crypto/rand, and a nil Getenv means no environment variables.
type Env struct {
	Version string
	Dir     string
	Stdout  io.Writer
	Stderr  io.Writer
	Getenv  func(string) string
	Now     func() time.Time
	Rand    io.Reader
}

// stamp is the time a write records: Env.Now, or time.Now when it is nil, in UTC and truncated to the millisecond.
func (e Env) stamp() time.Time {
	now := time.Now
	if e.Now != nil {
		now = e.Now
	}
	return now().UTC().Truncate(time.Millisecond)
}

// actorEnv is the environment variable that names the actor when --as is not given.
const actorEnv = "CAIRN_ACTOR"

// The prefixes of the lines on stderr: a failure's message, and each warning.
const (
	messagePrefix = "cairn: "
	warningPrefix = messagePrefix + "warning: "
)

// Flag names, without the leading "--".
const (
	flagJSON        = "json"
	flagRetracted   = "retracted"
	flagBranches    = "branches"
	flagAs          = "as"
	flagPersona     = "persona"
	flagModel       = "model"
	flagTitle       = "title"
	flagBody        = "body"
	flagStatus      = "status"
	flagTag         = "tag"
	flagMilestone   = "milestone"
	flagDue         = "due"
	flagDueBefore   = "due-before"
	flagKind        = "kind"
	flagSource      = "source"
	flagReason      = "reason"
	flagDisprovedBy = "disproved-by"
	flagOwnBranch   = "own-branch"
	flagLimit       = "limit"
	flagAll         = "all"
	flagClosed      = "closed"
	flagArchived    = "archived"
	flagOwed        = "owed"
	flagHTML        = "html"
)

const (
	flagPrefix = "--"
	endOfFlags = "--"
)

// switches are the flags that take no value. Every other flag takes one.
var switches = map[string]bool{flagJSON: true, flagRetracted: true, flagBranches: true, flagOwnBranch: true,
	flagAll: true, flagClosed: true, flagArchived: true, flagOwed: true}

// actorFlags are the flags every write takes.
var actorFlags = []string{flagAs, flagPersona, flagModel}

// command is one cairn command.
type command struct {
	flags   []string // the flags it takes besides --json, and actorFlags when it writes
	writes  bool
	args    int    // how many positional arguments it takes, or oneOrMore
	argsErr string // the usage error for any other number
	run     func(c *call) (output, error)
}

// oneOrMore as a command's args means it takes one positional argument or more.
const oneOrMore = -1

// milestoneCommand is the command whose first argument names a subcommand in milestoneCommands.
const milestoneCommand = "milestone"

// helpCommand, helpFlag and helpShort ask for help, as wantsHelp reads them.
const (
	helpCommand = "help"
	helpFlag    = flagPrefix + "help"
	helpShort   = "-h"
)

var commands = map[string]command{
	"version": {argsErr: "version takes no arguments", run: runVersion},
	"init":    {flags: []string{flagOwnBranch}, argsErr: "init takes no arguments", run: runInit},
	"new": {flags: []string{flagBody, flagTag, flagMilestone, flagDue}, writes: true,
		args: 1, argsErr: "new requires exactly one title argument", run: runNew},
	"edit": {flags: []string{flagTitle, flagBody, flagStatus, flagTag, flagMilestone, flagDue}, writes: true,
		args: 1, argsErr: "edit requires exactly one issue id", run: runEdit},
	"rm": {writes: true, args: 1, argsErr: "rm requires exactly one issue id", run: runRm},
	"show": {flags: []string{flagRetracted, flagBranches},
		args: 1, argsErr: "show requires exactly one issue id", run: runShow},
	"ls": {flags: []string{flagStatus, flagClosed, flagArchived, flagOwed, flagTag, flagMilestone, flagDueBefore,
		flagBranches, flagLimit, flagAll},
		argsErr: "ls takes no positional arguments", run: runLs},
	"comment": {flags: []string{flagKind, flagSource}, writes: true,
		args: 2, argsErr: "comment requires exactly one issue id and one text", run: runComment},
	"retract": {flags: []string{flagReason, flagDisprovedBy}, writes: true,
		args: 2, argsErr: "retract requires exactly one issue id and one comment id", run: runRetract},
	"purge": {writes: true,
		args: 2, argsErr: "purge requires exactly one issue id and one comment id", run: runPurge},
	"tags":       {argsErr: "tags takes no positional arguments", run: runTags},
	"milestones": {argsErr: "milestones takes no positional arguments", run: runMilestones},
	"search": {flags: []string{flagArchived, flagLimit, flagAll},
		args: oneOrMore, argsErr: "search requires at least one word", run: runSearch},
	"board": {flags: []string{flagHTML}, argsErr: "board takes no positional arguments", run: runBoard},
}

var milestoneCommands = map[string]command{
	"new": {flags: []string{flagDue}, writes: true,
		args: 2, argsErr: "milestone new requires exactly one id and one title", run: runMilestoneNew},
	"edit": {flags: []string{flagTitle, flagStatus, flagDue}, writes: true,
		args: 1, argsErr: "milestone edit requires exactly one milestone id", run: runMilestoneEdit},
}

// aliases are other names for commands.
var aliases = map[string]string{"--version": "version"}

// call is one run of a command: its environment, its parsed arguments, what it wrote, and the warnings it gives.
type call struct {
	env      Env
	dirGiven bool                // dirFlag was given
	args     []string            // the positional arguments
	values   map[string][]string // each value flag's values, in the order given
	switches map[string]bool     // the switches given
	wrote    *write              // what a write changed, set once it has
	warnings []string
	total    *int // the envelope's "total": set by ls and search only
}

// output is what a command prints when it succeeds: data with --json, text without.
type output struct {
	data any
	text string
}

// Run runs one command and returns its exit code.
func Run(args []string, env Env) int {
	jsonOn := scanJSON(args)
	c := &call{env: env}
	out, err := c.dispatch(args)
	if err == nil {
		err = c.print(out, jsonOn)
	}
	if err != nil {
		return c.fail(err, jsonOn)
	}
	for _, w := range c.warnings {
		fmt.Fprintln(env.Stderr, warningPrefix+w)
	}
	return ExitOK
}

// dirFlag, before the command, runs it as if Cairn started in the folder it names.
const dirFlag = "-C"

// chdir reads a dirFlag at the start of args: it sets Env.Dir to its folder, after Env.Dir when it is relative, with
// its links resolved, so that ".." after a link leaves the folder the link points to. It returns the arguments after
// it. The folder must exist.
func (c *call) chdir(args []string) ([]string, error) {
	if args[0] != dirFlag {
		return args, nil
	}
	if len(args) < 2 || strings.HasPrefix(args[1], flagPrefix) {
		return nil, usageError(dirFlag + " needs a folder")
	}
	dir := args[1]
	if !filepath.IsAbs(dir) {
		dir = c.env.Dir + string(filepath.Separator) + dir
	}
	dir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", dirFlag, err)
	}
	fi, err := os.Stat(dir)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", dirFlag, err)
	}
	if !fi.IsDir() {
		return nil, fmt.Errorf("%s: %s is not a folder", dirFlag, dir)
	}
	c.env.Dir, c.dirGiven = dir, true
	return args[2:], nil
}

// The user's config folder, as gh and the XDG Base Directory spec find it: $XDG_CONFIG_HOME when it is an absolute
// path, else $HOME/.config.
const (
	configHomeEnv = "XDG_CONFIG_HOME"
	homeEnv       = "HOME"
	configInHome  = ".config"
)

// openStore opens the tracker a command uses: the one store.Find reaches from Env.Dir, else, unless dirFlag was
// given, the global tracker that the user config file names (store.Global).
func (c *call) openStore() (*store.Store, error) {
	s, err := store.Find(c.env.Dir)
	if !errors.Is(err, store.ErrNoTracker) || c.dirGiven {
		return s, err
	}
	p := c.userConfigFile()
	if p == "" {
		return nil, err
	}
	return store.Global(p)
}

// userConfigFile is the user config file's path, or "" when the environment names no config folder.
func (c *call) userConfigFile() string {
	if c.env.Getenv == nil {
		return ""
	}
	dir := c.env.Getenv(configHomeEnv)
	if !filepath.IsAbs(dir) {
		home := c.env.Getenv(homeEnv)
		if !filepath.IsAbs(home) {
			return ""
		}
		dir = filepath.Join(home, configInHome)
	}
	return filepath.Join(dir, store.UserConfigFile)
}

// scanJSON reports whether args holds a standalone "--json" before any "--".
func scanJSON(args []string) bool {
	for _, a := range args {
		if a == endOfFlags {
			return false
		}
		if a == flagPrefix+flagJSON {
			return true
		}
	}
	return false
}

func (c *call) dispatch(args []string) (output, error) {
	if len(args) == 0 {
		return output{}, usageError("no command given (run `cairn help`)")
	}
	if wantsHelp(args) || len(args) > 2 && args[0] == dirFlag && args[2] == helpCommand {
		return runHelp(c)
	}
	args, err := c.chdir(args)
	if err != nil {
		return output{}, err
	}
	if len(args) == 0 {
		return output{}, usageError("no command given (run `cairn help`)")
	}
	if slices.Contains(beforeEnd(args), dirFlag) {
		return output{}, usageError(dirFlag + " goes before the command")
	}
	name, args := args[0], args[1:]
	if name == milestoneCommand {
		if len(args) == 0 {
			return output{}, usageError("milestone requires a subcommand: new or edit")
		}
		cmd, ok := milestoneCommands[args[0]]
		if !ok {
			return output{}, usageError(fmt.Sprintf("unknown milestone subcommand %q", args[0]))
		}
		return c.exec(milestoneCommand+" "+args[0], cmd, args[1:])
	}
	if alias, ok := aliases[name]; ok {
		name = alias
	}
	cmd, ok := commands[name]
	if !ok {
		return output{}, usageError(fmt.Sprintf("unknown command %q", name))
	}
	return c.exec(name, cmd, args)
}

// beforeEnd is args up to the first "--".
func beforeEnd(args []string) []string {
	if i := slices.Index(args, endOfFlags); i >= 0 {
		return args[:i]
	}
	return args
}

// wantsHelp reports whether args asks for help, as the package doc describes.
func wantsHelp(args []string) bool {
	if args[0] == helpCommand {
		return true
	}
	for _, a := range args {
		if a == endOfFlags {
			return false
		}
		if a == helpFlag || a == helpShort {
			return true
		}
	}
	return false
}

// exec runs cmd, which name names, with args, and then commits what it wrote.
func (c *call) exec(name string, cmd command, args []string) (output, error) {
	allowed := append([]string{flagJSON}, cmd.flags...)
	if cmd.writes {
		allowed = append(allowed, actorFlags...)
	}
	if err := c.parse(args, allowed); err != nil {
		return output{}, err
	}
	if !cmd.takes(len(c.args)) {
		return output{}, usageError(cmd.argsErr)
	}
	out, err := cmd.run(c)
	if err == nil && c.wrote != nil {
		c.commit(name)
	}
	return out, err
}

// takes reports whether n positional arguments are what cmd takes.
func (cmd command) takes(n int) bool {
	if cmd.args == oneOrMore {
		return n > 0
	}
	return n == cmd.args
}

// parse splits args into the positional arguments and the flags in allowed, as the package doc describes.
func (c *call) parse(args, allowed []string) error {
	c.values, c.switches = map[string][]string{}, map[string]bool{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == endOfFlags {
			c.args = append(c.args, args[i+1:]...)
			break
		}
		name, ok := strings.CutPrefix(a, flagPrefix)
		if !ok {
			c.args = append(c.args, a)
			continue
		}
		name, inline, hasInline := strings.Cut(name, "=")
		switch {
		case !slices.Contains(allowed, name):
			return usageError(fmt.Sprintf("unknown flag --%s", name))
		case switches[name] && hasInline:
			return usageError(fmt.Sprintf("flag --%s does not take a value", name))
		case switches[name]:
			c.switches[name] = true
		case hasInline:
			c.values[name] = append(c.values[name], inline)
		case i+1 < len(args) && !strings.HasPrefix(args[i+1], flagPrefix):
			i++
			c.values[name] = append(c.values[name], args[i])
		default:
			return usageError(fmt.Sprintf("flag --%s needs a value", name))
		}
	}
	return nil
}

// value is the last value given for a flag, or "" when it was not given.
func (c *call) value(name string) string {
	v, _ := c.given(name)
	return v
}

// given is the last value given for a flag, and whether it was given.
func (c *call) given(name string) (string, bool) {
	vs := c.values[name]
	if len(vs) == 0 {
		return "", false
	}
	return vs[len(vs)-1], true
}

// anyGiven reports whether any of the flags was given.
func (c *call) anyGiven(names ...string) bool {
	return slices.ContainsFunc(names, func(name string) bool { return len(c.values[name]) > 0 })
}

// actor is --as, or else the actorEnv environment variable. It is a usage error when both are empty.
func (c *call) actor() (string, error) {
	as := c.value(flagAs)
	if as == "" && c.env.Getenv != nil {
		as = c.env.Getenv(actorEnv)
	}
	if as == "" {
		return "", usageError("no actor: use --as or set " + actorEnv)
	}
	return as, nil
}

// warn adds a warning, which Run prints when the command succeeds.
func (c *call) warn(msg string) {
	c.warnings = append(c.warnings, msg)
}

// print writes out to stdout: its data in the success envelope with --json, else its text.
func (c *call) print(out output, jsonOn bool) error {
	if jsonOn {
		return writeJSON(c.env.Stdout, successJSON{OK: true, Data: out.data, Total: c.total, Warnings: c.warnings})
	}
	_, err := io.WriteString(c.env.Stdout, out.text)
	return err
}

// writeJSON writes v to w as one line of JSON, with <, > and & as they are.
func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

// fail reports err as the package doc describes and returns its exit code.
func (c *call) fail(err error, jsonOn bool) int {
	f := failureOf(err)
	if jsonOn {
		// Stdout has already failed if this does; the message still goes to stderr.
		_ = writeJSON(c.env.Stdout, failureJSON{Error: errorJSON{Code: f.code, Message: f.msg}})
	}
	fmt.Fprintln(c.env.Stderr, messagePrefix+f.msg)
	return f.exit
}

// Exit codes, as the package doc gives them.
const (
	ExitOK       = 0
	ExitError    = 1
	ExitUsage    = 2
	ExitNotFound = 4
)

// Error codes.
const (
	codeError     = "error"
	codeUsage     = "usage"
	codeNotFound  = "not_found"
	codeExists    = "exists"
	codeNoTracker = "no_tracker"
)

// failure is an error with its exit code and error code.
type failure struct {
	exit int
	code string
	msg  string
}

func (f *failure) Error() string { return f.msg }

func usageError(msg string) error    { return &failure{ExitUsage, codeUsage, msg} }
func notFoundError(msg string) error { return &failure{ExitNotFound, codeNotFound, msg} }
func existsError(msg string) error   { return &failure{ExitError, codeExists, msg} }

// noIssue turns store.ErrNotFound into the not_found failure for issueID, and returns any other error as it is.
func noIssue(err error, issueID string) error {
	if errors.Is(err, store.ErrNotFound) {
		return notFoundError(fmt.Sprintf("no issue %q", issueID))
	}
	return err
}

// noComment is the not_found failure for a comment that issueID does not have.
func noComment(commentID, issueID string) error {
	return notFoundError(fmt.Sprintf("no comment %q on issue %q", commentID, issueID))
}

// failureOf is err as a failure: itself when it is one, no_tracker or exists for those store errors, else "error".
func failureOf(err error) *failure {
	var f *failure
	switch {
	case errors.As(err, &f):
		return f
	case errors.Is(err, store.ErrNoTracker):
		return &failure{ExitError, codeNoTracker, "no " + store.DirName + " folder here or above"}
	case errors.Is(err, store.ErrExists):
		return &failure{ExitError, codeExists, err.Error()}
	}
	return &failure{ExitError, codeError, err.Error()}
}
