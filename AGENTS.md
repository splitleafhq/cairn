# AGENTS.md

Cairn is an issue tracker for coding agents first and people second. `docs/spec.md` says what it does, and
`docs/plan.md` gives the build order and how each step is made.

## Build and test
- Go 1.24 or later, standard library only unless `docs/plan.md` names a dependency.
- `scripts/check.sh` passes. It runs `gofmt -s -l .` and `go fix -diff ./...` (both must print nothing), `go vet`,
  `staticcheck -checks all` (pinned, run with `go run`, so it is not a module dependency) and `go test`.
  `lefthook install` runs it before every push (`lefthook.yml`).
- Names, doc comments and errors follow [Effective Go](https://go.dev/doc/effective_go) and
  [Go Code Review Comments](https://go.dev/wiki/CodeReviewComments). Staticcheck's ST checks enforce part of that;
  the reviewer checks the rest.

## Rules
- Tests are the spec. Each step's tests are written before its code and are never edited to make the code pass. If a
  test looks wrong, say so in the PR.
- Before writing or changing a test, read https://github.com/splitleafhq/.github/blob/main/docs/testing.md.
- Every command works without prompts or an editor, takes `--json`, and uses the exit codes in `docs/spec.md`.
- All writes go through `store.WriteFileAtomic`.
- No personal data, machine paths or names from anyone's private setup in code, tests, docs or fixtures. Examples use
  generic names with a `test-` prefix (test-agent, test-agent-2, test-persona, test-model), so synthetic names are easy
  to find. An older example changes only when a change already edits that example.
- Commits and PRs follow the account's
  [repo standards](https://github.com/seriouslysean/.github/blob/main/docs/repo-standards.md#issues-and-pull-requests):
  `type(scope): summary`, where type is feat, fix, refactor, docs or chore. feat, fix and refactor need a GitHub issue
  first, on a branch `<issue>-<slug>`. Anything else goes on `<type>/<slug>`, and its PR says `No issue: <type>`.
- One plan step per PR, closing the step's issue. The step's tests-first commit is `chore(test): …`, since a commit of
  tests alone is a chore.
- This repository's issues are in GitHub Issues. It keeps no `.cairn/` tracker until Cairn can sync with GitHub.
