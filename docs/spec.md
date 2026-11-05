# Cairn v0 spec

Cairn is an issue tracker for coding agents first and people second. Issues live as markdown files in a `.cairn/`
folder inside the repository they belong to, committed with the code or on a branch of their own (below), or in one
global tracker for work outside any repository's tracker. One `cairn`
command, installed once per machine, reads and writes them. An agent can do everything without a person: no prompts,
no editor, JSON on request.

## Storage

```
.cairn/
  config.yml               statuses, tag groups, GitHub repo, embeddings URL, commit on write
  issues/7f3k/issue.md     one folder per issue
  issues/7f3k/c-x9k2.md    one file per comment
  milestones/v0.4.md       one file per milestone
  .index/                  search cache, gitignored, rebuilt from the files
  .gitignore               keeps .index/ and the temporary files of writes out of git
```

- Every file starts with frontmatter: a strict subset of YAML (see `internal/doc`), with flat keys and values that are
  plain text, a double-quoted string, or a flow list `[a, b]`.
- A comment is its own file, so two branches that comment on the same issue never conflict when they merge.
- Every write replaces the whole file: it goes to a temporary file in the same folder, which is then renamed into
  place. When two writers edit the same file at once, the last write wins. There is no lock and no queue.
- The temporary file's name starts with `.cairn-tmp-`, and the `.gitignore` that `cairn init` writes has the line
  `.cairn-tmp-*`, so git never sees one, and commit on write never commits another writer's half-written file. A
  tracker made before that line existed gets it by hand: add `.cairn-tmp-*` to `.cairn/.gitignore`.

## Where Cairn finds its tracker
1. `cairn -C <dir> <command> …` runs as if Cairn started in `<dir>`, which is read from the current folder when it is
   relative, and never uses the global tracker. Links in `<dir>` are resolved before any `..` in it, as `cd` would.
   `-C` is read only before the command; anywhere after it, a flag's value included, it is a usage error. `--help`
   and `-h` still win.
2. Otherwise, the first `.cairn` found walking up from the current folder, as "A branch of its own" says for linked
   worktrees: a repository's own tracker always wins. A `.cairn` that cannot be checked, as when it is a link into a
   folder that cannot be read, is an error, never a reason to go on to the global tracker.
3. Otherwise, the global tracker: the `.cairn` in the folder that the user config file names with its `tracker` key.
4. Otherwise, the `no_tracker` error.

The user config file is `$XDG_CONFIG_HOME/cairn/config.yml` when `XDG_CONFIG_HOME` is an absolute path, else
`$HOME/.config/cairn/config.yml`, as `gh` finds its own (`gh help environment`). It has the same frontmatter as a
tracker's `config.yml`, and Cairn reads only its `tracker` key, an absolute path, with links resolved as for `-C`:

```
---
tracker: /home/test-agent/notes
---
```

- With no user config file, or one with no `tracker` key, there is no global tracker.
- Cairn reads the file only when step 3 is reached, so a wrong one never stops a repository's own tracker. A file
  that cannot be read or parsed, a `tracker` that is not an absolute path, and a folder that cannot be found or has no
  `.cairn` folder are then an error (exit 1) that names the file.
- `cairn init` makes `.cairn` in the folder it runs in, or in `-C <dir>`, and never looks at the global tracker.
- Not yet: an environment variable that overrides the user config file.

### Issue
```
---
id: 7f3k
title: "Export drops the last row"
status: in-progress
tags: [severity:high, effort:2, area:export]
milestone: v0.4
due: 2026-11-01
by: alice
model: example-model-1
created: 2026-10-17T19:00:00.000Z
updated: 2026-10-17T19:20:00.000Z
github: 123
---
What is wrong, why it matters, and what "done" means.
```
- `by`, `persona` and `model` say who created the issue. `new` sets them once; edits and sync never change them. Who
  edited an issue is not recorded.
- An issue without them was made before Cairn recorded creators, and its creator is unknown: nothing fills them in
  later.
- They are what the writer gave, not checked identities. `model` names the model that ran the command, not
  necessarily the author of every word.
`github` appears only after a sync. Dates are `YYYY-MM-DD`. Times are UTC RFC 3339 with exactly three fraction
digits, such as `2026-10-17T19:20:00.000Z`, so every time is 24 characters and times sort the same as text as they
do as times. Files written by v0.1.0, which dropped trailing zeros, still load.
Frontmatter lines may end in CRLF, as in a Windows checkout. Cairn writes them with LF and keeps a body as it is.

### Comment
```
---
id: c-x9k2
kind: evidence
by: alice
persona: skeptic
model: example-model-1
at: 2026-10-17T19:20:00.000Z
source: export_test.go:42
---
The last row is dropped when the file has no trailing newline.
```
A retracted comment adds `retracted_by`, `retracted_at`, `retracted_reason` and, optionally, `disproved_by: c-ab12`.

### Milestone
Same frontmatter as an issue (`id`, `title`, `status`, `due`), and a body. It has no comments. Its status is one of the
config's statuses. Commands keep the body but don't write it; it is edited by hand. Its id is its file name without
`.md`: not empty, with no `/`, and not starting with `.`. The milestone an issue names follows the same rule, or is
empty. `milestone new` refuses an id that differs only in case from one that has a file, since the two name one file
on a case-insensitive file system.

### Config
`config.yml` is frontmatter with an empty body, like every other file:
```
---
statuses: [backlog, open, in-progress, done]
closed: [done]
archived: [archived]
tag_groups: [severity, effort, area]
github_repo: owner/name
embeddings_url: http://127.0.0.1:8080/v1/embeddings
embeddings_model: model-name
embeddings_prompt: "task: sentence similarity | query: "
retracted_match: 0.6
commit: false
---
```
Every key is optional. Shown above with the defaults, except that `github_repo`, `embeddings_url`, `embeddings_model`
and `embeddings_prompt` are off when absent. `commit` is `true` or `false`.

`embeddings_model` is sent to the endpoint as `model`, and is left out when empty. `embeddings_prompt` is put before
every text before it is embedded, and is empty by default. Some models need one: measured on one tracker, EmbeddingGemma
put all four known duplicates first with `task: sentence similarity | query: `, and one fell to rank 4 without it. A
value with a space at its end is written in double quotes, as above.

`closed` and `archived` name the statuses that count as closed, finished work, and archived, kept for history but not
work (see "Closed and archived issues" below). A status in both counts as archived, a name that is not one of the
`statuses` matches nothing, and an empty list `[]` names none. With the defaults, `done` is closed, and a tracker
counts issues as archived only once its `statuses` list `archived`.

## IDs
- Issues: 4 lowercase characters from Crockford's base32 alphabet (`0-9a-z` without `i l o u`), checked against existing
  ids. The length grows to 5 once a tracker holds 1,000 issues, and to 6 at 32,000.
- Comments: `c-` plus 4 characters, unique within their issue.
- The file or folder name is the id, so changing a title never moves a file.

## Commands

| Need | Command |
|---|---|
| Start | `cairn init [--own-branch]` |
| Lists | `cairn tags`, `cairn milestones`, `cairn ls [--status s] [--closed] [--archived] [--owed] [--tag t] [--milestone m] [--due-before d] [--branches] [--limit N \| --all]` |
| Milestones | `cairn milestone new <id> "title" [--due d]`, `cairn milestone edit <id> [--title t] [--status s] [--due d]` |
| Add | `cairn new "title" [--body text] [--tag t]... [--milestone m] [--due d]` |
| Edit | `cairn edit <id> [--title t] [--body text] [--status s] [--tag +t] [--tag -t] [--milestone m] [--due d]` |
| Delete | `cairn rm <id>` |
| Read | `cairn show <id> [--retracted] [--branches]` |
| Comment | `cairn comment <id> --kind evidence\|decision\|result [--source s] "text"` |
| Retract | `cairn retract <id> <comment> --reason "…" [--disproved-by <comment>]` |
| Purge | `cairn purge <id> <comment>` |
| Search | `cairn search "words" [--archived] [--limit N \| --all]`, `cairn search --meaning "text"` |
| Board | `cairn board [--html <file>]` |
| Sync | `cairn pull`, `cairn push [--dry-run]` |
| Help | `cairn help`, `cairn version` |

`cairn search --meaning`, `cairn pull` and `cairn push` are in the table above but are not built yet. They come in
later steps of [plan.md](plan.md). Until then they exit 2, a usage error. `cairn help` lists only what is built.

- Every write takes `--as <actor>` and, optionally, `--persona <p>` and `--model <m>`. The actor can also come from the
  `CAIRN_ACTOR` environment variable. `new` and `comment` record all three as `by`, `persona` and `model`; `retract`
  records the actor as `retracted_by`.
- Every command takes `--json`. On success it prints `{"ok": true, "data": …}`, plus `"warnings": ["…"]` when it
  gave warnings, the same ones stderr gets, and on `ls` and `search` `"total"`. On failure it prints
  `{"ok": false, "error": {"code": "…", "message": "…"}}` on stdout, and a one-line message on stderr. `<`, `>` and
  `&` come out as they are, not escaped.
- `ls` and `search` print at most 30 rows: `--limit N` prints at most N, and `--all` every row. The cut comes after
  the filters and the sort. `ls` keeps its last rows, the newest issues and then the branches' rows, and `search` its
  best matches. When rows are left out, the command warns `30 of 672 shown; --limit N or --all for more`. `"total"`
  is how many rows there were before the cut, whether or not any were cut. There are no pages: narrow with a filter
  or a better query.
- `ls` lists open work, and `search` leaves out archived issues, as "Closed and archived issues" below says.
- Exit codes:
  - 0: ok;
  - 1: error;
  - 2: usage;
  - 4: not found (3 is not used).
- `help` prints a line that says what `--json` prints, one usage line per command, then the rules below, word for
  word. With `--json` its data is `{"usage": [...], "rules": [...]}`. `help` with any arguments, and `--help` or `-h`
  on any command before `--`, print the same and exit 0.

## Rules
An agent with only the binary learns these from `cairn help`:
- cairn new lists the existing issues nearest its title as warnings. If one is the same work, comment there and remove the new issue. Run cairn search first anyway: it also reads bodies.
- Comment on an issue someone else owns rather than editing it: comments never conflict across branches.
- evidence needs --source, decision quotes the owner, result says what was done.
- To close an issue, add a result comment, then cairn edit <id> --status done.
- Retract a wrong comment instead of deleting it. purge removes a comment that must not stay: a pasted secret, or one that adds nothing.
- One deliverable per issue: a decision that adds another deliverable becomes a new issue that names its parent.
- When you stop before an issue is done and you did work on it, add an evidence comment that says what is done and what is left.
- To park an issue on purpose, add a result comment that names what brings it back, and leave it open.

## Closed and archived issues
- `ls` leaves out closed issues unless given `--closed`, and archived issues unless given `--archived`. The config's
  `closed` and `archived` say which statuses those are.
- `ls --status s` lists the issues whose status is `s`, whatever it is: `--status done` and `--status archived` need
  no other flag.
- `search` searches closed issues, but leaves out archived ones unless given `--archived`.
- `show <id>` reads any issue, closed and archived ones included. `tags` and `milestones` count every issue.
- `--all` only lifts the row cap: `ls --all` lists every open issue, and `ls --closed --archived --all` every issue.
- With `--branches`, the branches' rows go through the same filter, with the checkout's config.

## Comments are data, not chat
- A writer may add three kinds, and `comment` refuses anything else (`pull` also creates kind `github`, below):
  - `evidence` requires `--source`: a URL, a file and line, a log path or a command;
  - `decision` holds the owner's words, quoted;
  - `result` is what was done: a PR, a commit or test output.
- `ls --owed` lists the issues whose latest active decision has no active result with a later `at`: what the owner
  approved and nobody has recorded as done. Evidence does not count, and an issue with no decision is not owed. A
  decision that needs nothing built gets a result that says so, and so does a parked issue, naming what brings it back.
- `retract` keeps the comment where it is and adds the retracted fields.
- `show` leaves retracted comments out, in text and in `--json`. `show --retracted` includes them, marked.
- A new comment whose word-overlap score (below) with a retracted comment on the same issue reaches `retracted_match`
  is still added, with a warning that names the retracted comment and its reason. With `--json` its data lists them
  as `retracted_matches`.
- `purge` deletes the comment file, for a comment that must not stay: a pasted secret, or one that adds nothing
  (a repeat, a note that only says who took or released the issue). Git history still holds it.

## Search
- `search` scans titles, bodies and active comments for the words given, and ranks by how many match.
- `--meaning` is optional:
  - It needs `embeddings_url`, an OpenAI-compatible `/v1/embeddings` endpoint.
  - Vectors are cached in `.cairn/.index/`, keyed by each file's hash.
  - Search compares the query vector with every cached vector (cosine similarity).
- The word-overlap score of two texts is the number of distinct words they share divided by the number of distinct
  words in either. A word is a run of letters and digits, compared in lowercase.
- Cairn does not decide what a duplicate is. `new` lists the nearest titles (see "Similar issues on new"), and
  `search` also reads bodies and comments, so run it before `new` too.

## Similar issues on new
`new` files the issue exactly as before, then compares its title with the title of every other issue in the checkout,
closed ones included (a repeat can be a regression) and archived ones left out. It warns of the nearest 3, highest
score first, ties by id, and exits 0. It never refuses and has no `--force`: the reader decides. Titles alone are
compared, because bodies and tags differ between two agents who file the same work; measured on one tracker of about
930 issues, title embeddings ranked all four known duplicates first, and no score threshold separated them from
unrelated titles that share a prefix, so none is used. Issues on other branches are not compared.
- With `embeddings_url`, the score is the cosine similarity of the titles' vectors, and a warning says
  `similar issue pdev (title meaning 0.68): Agent portal: phone view`. Vectors come from the endpoint in batches, with
  10 seconds for all of them, and are cached in `.cairn/.index/`, keyed by a hash of the url, model, prompt and title.
  `edit --title` caches the new title's vector as it saves, so retitling issues does not leave the next `new` to fetch
  them all; when that fails, `edit` warns `title vector not cached: <error>` and still exits 0.
- Without it, or when the endpoint fails, the score is `words.Overlap` of the titles: `similar issue jzfa (word
  overlap 0.40): …`. When the endpoint was set and failed, one more warning comes first and says why.
- When at least one issue is listed, a last warning says what to do with a duplicate, the new issue's id filled
  in: `if 7dys is the same work as one of these: put what 7dys adds on that issue as evidence, then run: cairn rm
  7dys`.
- A score of 0 is not listed. Nothing in the check can make `new` fail.
- The warnings are the list. `new`'s data stays the issue.

## Board
`cairn board` is the tracker as a board for a person to look at, as `ls` and `show` are for an agent. It only reads.
- One column per status, in the order of the config's `statuses`, empty ones included. A status that an issue has and
  the config lacks, as after a change to the config, gets a column after those, in byte order.
- A column lists its issues with the last update first. A card is an issue's id, title, tags and last update.
- A column is collapsed when the config's `closed` or `archived` names its status, the statuses `ls` leaves out unless
  asked: a terminal prints its count and not its issues, and the page starts it shut. With the defaults that is `done`.
- `cairn board` prints a section per column: a header, `backlog (179)`, then a line per issue with its id, last
  update, tags and title, separated by tabs, the title last as in `ls`. A collapsed column's header says so,
  `done (476, collapsed)`, and it has no lines. With `--json`, `data.columns` lists each column's `status`,
  `collapsed`, `count` and `issues`, empty for a collapsed column.
- `cairn board --html <file>` writes one page that shows the whole board, the cards of collapsed columns included, each
  column a `<details>` element that is open unless it is collapsed. A relative name is relative to the folder Cairn
  runs in, and the file is replaced, a link included, with the mode of the tracker's own files. A folder as the name is
  an error. To refresh the page, run the command again.
- The page is one fixed template compiled into the binary, and a run fills in data and changes nothing else, so the
  page never needs rewriting when the data changes. It is read-only and has no script. A Content-Security-Policy in
  it allows inline CSS and nothing else: the page loads nothing from the network, and the file works offline.
  Titles and tags are escaped.
- Not yet: boards of several trackers (`board [dir...]`), grouping by project, `cairn serve`, and a cap on how many
  cards a terminal board prints.

## Branches
- By default, commands read the current checkout only. Only `--branches`, commit on write and `init` (below) run git.
- `ls --branches` and `show --branches` add what active branches committed. The default branch is the one
  `origin/HEAD` names, else `main`, else `master`. An active branch is a local branch, in any worktree, that:
  - is not the default branch or the branch checked out here;
  - shares history with the default branch, and is not yet merged into it;
  - has a last commit less than 14 days old;
  - changed files in `.cairn/issues/` since it left the default branch.
- From each active branch, every issue it changed is read whole, the issue and its comments, as its last commit has
  it. Uncommitted work, here or in another worktree, is not seen. An issue the branch removed is left out.
- `ls` lists the checkout's issues, then each branch's, marked with the branch name. `show` prints the checkout's
  version, then each branch's under a `=== branch <name> ===` line, so an issue changed on two branches shows both.
- On a tracker on its own branch (below), `--branches` adds nothing: no other branch holds its issues.

## A branch of its own (optional)
- `cairn init --own-branch` keeps the tracker on a branch named `cairn`, in a git repository of its own at `.cairn/`,
  which the code repository ignores through a `/.cairn` line in `.git/info/exclude`, a file git never commits. Writes
  commit on `cairn` whatever branch the code has checked out, so they never ride in a pull request and outlive a code
  branch that is dropped.
- It runs in the top folder of the main checkout. When `origin/cairn` holds a tracker, it attaches it with
  `git clone --single-branch --branch cairn`. Otherwise it makes one, with commit on write on, the code's `origin` as
  its remote, and one commit of its files. It never pushes: `git -C .cairn push` shares the writes, and
  `git -C .cairn pull` brings in other people's.
- `cairn init` without the flag will not start a second tracker: not in a checkout's top folder where `origin/cairn`
  holds one (it names the command that attaches it), and not where a tracker on its own branch already serves the
  folder.
- A linked worktree, inside the repository or outside it, uses the main checkout's tracker when it has no `.cairn` of
  its own. Cairn follows the worktree's `.git` file to the `commondir` file git keeps for it, without running git. A
  bare repository's worktrees have no main checkout, so they find none.
- What it costs:
  - a shallow clone, or a host that clones one branch, has no `origin/cairn`, so it has no tracker until one is
    attached;
  - an agent that can push only its own branch cannot push `cairn`;
  - `git clean -ffdx` in the code repository deletes `.cairn/`, with any commits not yet pushed;
  - a change to `config.yml` is committed by hand;
  - on a public repository, the `cairn` branch is public too.

## Commit on write (optional)
- With `commit: true`, each write that succeeds commits what it changed: the issue's folder, or for `milestone new`
  and `milestone edit` the milestone's file, as it is on disk. Nothing else is staged or committed. `init` commits
  nothing, except the first commit of `init --own-branch`.
- The message is `chore(cairn): <command> <id> (by <actor>)`, for example `chore(cairn): comment 7f3k (by alice)`.
  The author is whoever git's config names, and the hooks of the repository the tracker's folder is in run: on a
  branch of its own, that is the tracker's own repository, not the code's.
- A commit is tried once. When it fails (no git, no repository, another writer's lock, a hook), the write stands and
  the command succeeds with a warning, unless another writer has already committed the change. The next write to the
  same issue commits what was left.
- Two writes at the same moment can collide on git's locks, and then one of them is left uncommitted. To commit every
  write, run writes one at a time.
- It commits on the branch checked out, so it suits a tracker kept in a repository of its own, or on a branch of its
  own, where it is on. In a repository that works through pull requests, with the tracker in the tree, leave it off.

## GitHub sync (optional, through the `gh` CLI)
- `pull`:
  - New GitHub issues become Cairn issues with `github: N` and `by: github:<login>` for their GitHub author, with no
    persona or model: the actor running `pull` is not their creator.
  - For issues already linked, state and labels update.
  - The title and body belong to Cairn: edits made on GitHub are reported, not merged.
  - GitHub comments arrive as comments of kind `github`, with `by: github:<login>`.
- `push`:
  - Issues without `github` are created on GitHub. On GitHub their author is whoever owns the `gh` login; `by` stays
    as it is.
  - Linked issues get their title, body, labels and state updated, and their new active comments posted.
  - Issues closed on GitHub are skipped and reported.
  - `--dry-run` prints what would change.
- Cairn never pushes on its own. Pushing is the user's call.

## Not in v0
Locks, leases or claims; scheduled jobs; a server; hiding GitHub comments that were already posted before being
retracted.
