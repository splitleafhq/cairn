# Cairn

An issue tracker for coding agents first and people second. Issues are markdown files in a `.cairn/` folder inside
your repository, committed with your code. One `cairn` command, installed once per machine, reads and writes them,
and every command can answer in JSON.

Status: early alpha. `cairn help` lists the commands that are built. See [docs/plan.md](docs/plan.md) for what comes
next.

## Install

Needs Go 1.24 or later.

```sh
go install github.com/splitleafhq/cairn/cmd/cairn@latest
cairn version
```

`go install` puts `cairn` in `$(go env GOPATH)/bin`, or in `$(go env GOBIN)` when that is set. That folder has to be on
your `PATH`, or `cairn version` says "command not found".

## Start

```sh
cd your-repo
cairn init
export CAIRN_ACTOR=alice    # or pass --as alice to each write
cairn new "Export drops the last row" --tag area:export    # prints the new id, such as 7f3k
cairn ls
cairn comment 7f3k --kind evidence --source parser.go:42 "Rows are copied twice"
cairn edit 7f3k --status done
cairn search export row
```

Commit `.cairn/` with your code, or keep it out of your code's history on a branch of its own:

```sh
cairn init --own-branch    # in the main checkout's top folder; attaches origin/cairn when it holds a tracker
git -C .cairn push         # shares the writes; git -C .cairn pull brings in other people's
```

Every worktree of the repository then uses that one tracker. See "A branch of its own" in
[docs/spec.md](docs/spec.md).

An agent working in a folder with no tracker of its own can use one tracker for the whole machine. Name its folder
once, in `~/.config/cairn/config.yml` (or `$XDG_CONFIG_HOME/cairn/config.yml`):

```
---
tracker: /home/test-agent/notes
---
```

Outside any `.cairn`, every command but `init` then uses `/home/test-agent/notes/.cairn`. `cairn -C <dir> <command>` runs as
if started in `<dir>`, and never falls back to the global tracker. See "Where Cairn finds its tracker" in [docs/spec.md](docs/spec.md).

## For agents

Put this in your repository's AGENTS.md:

```md
## Issues

This repo tracks issues with Cairn. Run `cairn help` before using it, and set `CAIRN_ACTOR` to your name.
```

`cairn help` prints every command's usage and the rules. Add `--json` to any command: success prints
`{"ok": true, "data": …}`, plus `"warnings"` when there are any and `"total"` on `ls` and `search`, failure prints
`{"ok": false, "error": {"code": "…", "message": "…"}}`, and the exit codes are 0 ok, 1 error, 2 usage, 4 not found.
`ls` lists open work: `--closed` adds done issues, `--archived` adds archived ones, and `--status s` lists one
status, whatever it is. `ls --owed` lists the issues whose latest decision has no result after it: work that was
approved and not yet done. `search` leaves archived issues out unless given `--archived`. `ls` and `search` print at
most 30 rows, with a warning when they leave some out: narrow with a filter or a better query, or pass `--limit N` or
`--all`. [docs/spec.md](docs/spec.md) has the rest.

## For people

`cairn board` prints the tracker as one section per status, in the order of the config's `statuses`. Closed and
archived statuses are collapsed to a count. `cairn board --html board.html` writes the same board as one page that
loads nothing and runs no script, so it opens from a file. The page's code is fixed in the binary and the command only
fills in the data: to refresh it, run the command again.

## Requests

Open an issue at https://github.com/splitleafhq/cairn/issues and say what you were trying to do.

License: MIT.
