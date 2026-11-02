package cli

import (
	"strings"

	"github.com/splitleafhq/cairn/internal/store"
)

func runVersion(c *call) (output, error) {
	return output{versionJSON{Version: c.env.Version}, "cairn " + c.env.Version + "\n"}, nil
}

func runHelp(*call) (output, error) {
	const indent = "\n  "
	text := "Usage: cairn [" + dirFlag + " <dir>] <command> [args]\n" +
		"Cairn uses the .cairn here or in a folder above. Outside one, it uses the global tracker: the .cairn in the " +
		"folder named by the line \"tracker: <absolute path>\", between two \"---\" lines, in $" + configHomeEnv + "/" +
		store.UserConfigFile + ", else ~/" + configInHome + "/" + store.UserConfigFile + ". " + dirFlag +
		" <dir> runs as if started in <dir>, and never uses the global tracker.\n" +
		`Every command takes --json: success prints {"ok": true, "data": …}, plus "warnings" when there are any and ` +
		`"total" on ls and search; failure prints {"ok": false, "error": {"code": "…", "message": "…"}}.` + "\n" +
		"Writes take --as (or " + actorEnv + "), --persona and --model.\n\n" +
		"Commands:" + indent + strings.Join(commandUsage, indent) + "\n\n" +
		"Rules:" + indent + strings.Join(agentRules, indent) + "\n"
	return output{helpJSON{Usage: commandUsage, Rules: agentRules}, text}, nil
}

// commandUsage is one usage line per command, in the order help prints them.
var commandUsage = []string{
	`init [--own-branch]`,
	`new "title" [--body text] [--tag t]... [--milestone m] [--due YYYY-MM-DD]`,
	`edit <id> [--title t] [--body text] [--status s] [--tag +t|-t]... [--milestone m] [--due YYYY-MM-DD]`,
	`rm <id>`,
	`show <id> [--retracted] [--branches]`,
	`ls [--status s] [--closed] [--archived] [--owed] [--tag t]... [--milestone m] [--due-before YYYY-MM-DD] [--branches] [--limit N | --all]`,
	`comment <id> --kind evidence|decision|result [--source s] "text"`,
	`retract <id> <comment> --reason "text" [--disproved-by <comment>]`,
	`purge <id> <comment>`,
	`tags`,
	`milestones`,
	`milestone new <id> "title" [--due YYYY-MM-DD]`,
	`milestone edit <id> [--title t] [--status s] [--due YYYY-MM-DD]`,
	`search <words>... [--archived] [--limit N | --all]`,
	`board [--html <file>]`,
	`version`,
	`help`,
}

// agentRules are the rules for using Cairn that docs/spec.md gives, word for word.
var agentRules = []string{
	"Run cairn search before cairn new: Cairn does not look for duplicates.",
	"Comment on an issue someone else owns rather than editing it: comments never conflict across branches.",
	"evidence needs --source, decision quotes the owner, result says what was done.",
	"To close an issue, add a result comment, then cairn edit <id> --status done.",
	"Retract a wrong comment instead of deleting it. purge is only for mistakes like a pasted secret.",
	"One deliverable per issue: a decision that adds another deliverable becomes a new issue that names its parent.",
	"When you stop before an issue is done, add an evidence comment that says what is done and what is left.",
	"To park an issue on purpose, add a result comment that names what brings it back, and leave it open.",
}
