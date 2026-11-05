package cli

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

var helpUsage = []string{
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

var helpRules = []string{
	"cairn new lists the existing issues nearest its title as warnings. If one is the same work, comment there and remove the new issue. Run cairn search first anyway: it also reads bodies.",
	"Comment on an issue someone else owns rather than editing it: comments never conflict across branches.",
	"evidence needs --source, decision quotes the owner, result says what was done.",
	"To close an issue, add a result comment, then cairn edit <id> --status done.",
	"Retract a wrong comment instead of deleting it. purge is only for mistakes like a pasted secret.",
	"One deliverable per issue: a decision that adds another deliverable becomes a new issue that names its parent.",
	"When you stop before an issue is done, add an evidence comment that says what is done and what is left.",
	"To park an issue on purpose, add a result comment that names what brings it back, and leave it open.",
}

// help is the only documentation an agent with just the binary has, so it gives every command's usage and the rules.
func TestHelpText(t *testing.T) {
	empty := &tracker{t: t, dir: t.TempDir(), now: t0, rnd: &seq{}}
	want := "Usage: cairn [-C <dir>] <command> [args]\n" +
		"Cairn uses the .cairn here or in a folder above. Outside one, it uses the global tracker: the .cairn in the " +
		"folder named by the line \"tracker: <absolute path>\", between two \"---\" lines, in " +
		"$XDG_CONFIG_HOME/cairn/config.yml, else ~/.config/cairn/config.yml. " +
		"-C <dir> runs as if started in <dir>, and never uses the global tracker.\n" +
		"Every command takes --json: success prints {\"ok\": true, \"data\": …}, plus \"warnings\" when there are any and " +
		"\"total\" on ls and search; failure prints {\"ok\": false, \"error\": {\"code\": \"…\", \"message\": \"…\"}}.\n" +
		"Writes take --as (or CAIRN_ACTOR), --persona and --model.\n\n" +
		"Commands:\n  " + strings.Join(helpUsage, "\n  ") + "\n\n" +
		"Rules:\n  " + strings.Join(helpRules, "\n  ") + "\n"
	for _, args := range [][]string{{"help"}, {"--help"}, {"-h"}} {
		if code, out, errOut := empty.run(args...); code != 0 || out != want || errOut != "" {
			t.Fatalf("%q: exit %d, %q\ngot\n%s\nwant\n%s", args, code, errOut, out, want)
		}
	}
}

func TestHelpJSON(t *testing.T) {
	empty := &tracker{t: t, dir: t.TempDir(), now: t0, rnd: &seq{}}
	code, data, _ := empty.json("help")
	var got map[string][]string
	if err := json.Unmarshal(data, &got); code != 0 || err != nil {
		t.Fatalf("exit %d, %s, %v", code, data, err)
	}
	want := map[string][]string{"usage": helpUsage, "rules": helpRules}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}

// An agent's first guesses at help get it: help with any arguments, and --help or -h on any command, before "--".
func TestHelpAnywhere(t *testing.T) {
	empty := &tracker{t: t, dir: t.TempDir(), now: t0, rnd: &seq{}}
	_, want, _ := empty.run("help")
	for _, args := range [][]string{
		{"help", "new"},
		{"help", "--tag", "x"},
		{"new", "--help"},
		{"new", "title", "-h"},
		{"ls", "--status", "-h"},
		{"milestone", "new", "--help"},
		{"nosuch", "-h"},
	} {
		if code, out, errOut := empty.run(args...); code != 0 || out != want || errOut != "" {
			t.Errorf("%q: exit %d, stderr %q\ngot\n%s\nwant\n%s", args, code, errOut, out, want)
		}
	}

	_, wantData, _ := empty.json("help")
	if code, data, errOut := empty.json("new", "--help"); code != 0 || string(data) != string(wantData) || errOut != "" {
		t.Errorf("new --help --json: exit %d, stderr %q, data %s, want %s", code, errOut, data, wantData)
	}

	// After "--", -h is an argument like any other.
	if code, out, _ := empty.run("show", "--", "-h"); code == 0 || out != "" {
		t.Errorf("show -- -h: exit %d, out %q; want a failure, not help", code, out)
	}
}
