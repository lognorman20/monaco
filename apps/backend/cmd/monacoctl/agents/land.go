package agents

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/errs"
)

const (
	stackFields = `number state mergeable closedAt baseRefName headRefName headRefOid body mergeCommit{oid} ` + labelFields + `
commits(last:1){nodes{commit{` + commitChecks + `}}}
timelineItems(itemTypes:[UNLABELED_EVENT],last:20){nodes{__typename ... on UnlabeledEvent{createdAt label{name}}}}`
	settleAfter = time.Minute
	repoQuery   = "query($owner:String!,$name:String!){repository(owner:$owner,name:$name){"
)

type Queue struct {
	Top int       `json:"top"`
	PRs []int     `json:"prs"`
	At  time.Time `json:"at,omitzero"`
}

type Arm struct {
	Top int       `json:"top"`
	PRs []int     `json:"prs"`
	At  time.Time `json:"at"`
}

type stackPR struct {
	gqlPR
	Base        string `json:"baseRefName"`
	Head        string `json:"headRefName"`
	Mergeable   string `json:"mergeable"`
	MergeCommit struct {
		OID string `json:"oid"`
	} `json:"mergeCommit"`
}

func landStackCmd(ctx context.Context, env *Env, args []string, stdout io.Writer) error {
	n, err := prArg(args, "land-stack <top-pr>")
	if err != nil {
		return err
	}
	tops, err := env.stackPulls(ctx, []int{n})
	if err != nil {
		return err
	}
	ticket, ok := PR{Body: tops[0].Body}.Ticket()
	if !ok {
		return landErr(fmt.Sprintf("#%d links no ticket; its body needs \"Part of #N\" or \"Closes #N\"", n))
	}
	rec, err := env.record(ctx, ticket)
	if err != nil {
		return err
	}
	if rec.Queued != nil {
		if rec.Queued.Top != n {
			return landErr(fmt.Sprintf("#%d already has #%d queued", ticket, rec.Queued.Top))
		}
		if done, err := env.settleQueued(ctx, rec, stdout); done || err != nil {
			return err
		}
		rec.Queued = nil
	}
	stack, dir, err := env.stackOf(ctx, rec, n, stdout)
	if err != nil {
		return err
	}
	if err := env.flowGate(ctx, rec, stack); err != nil {
		return err
	}
	if waiting := waitingOn(stack); len(waiting) > 0 {
		return env.arm(ctx, rec, stack, waiting, stdout)
	}
	return env.land(ctx, rec, dir, stack, stdout)
}

func (env *Env) arm(ctx context.Context, rec Record, stack []stackPR, waiting []string, stdout io.Writer) error {
	top := stack[len(stack)-1].Number
	if blocker(stack) != "" {
		_, _ = fmt.Fprintf(stdout, "not landing #%d; waiting on %s\n", top, strings.Join(waiting, ", "))
		return nil
	}
	rec.Armed = &Arm{Top: top, PRs: numbers(stack), At: env.Now()}
	rec.Changed = env.Now()
	if err := env.storeRecord(ctx, rec); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(stdout, "armed #%d; agents watch lands it once stage 1 passes (waiting on %s)\n",
		top, strings.Join(waiting, ", "))
	return nil
}

func blocker(stack []stackPR) string {
	for _, p := range stack {
		if p.Mergeable == conflicting {
			return conflictLine(p.Number, p.Base)
		}
	}
	for _, p := range stack {
		if p.flat("").Stage1 == "failure" {
			return fmt.Sprintf("#%d stage 1 failed", p.Number)
		}
	}
	return ""
}

func (env *Env) landArmed(ctx context.Context, r Record) []string {
	top := r.Armed.Top
	var out strings.Builder
	stack, dir, err := env.stackOf(ctx, r, top, &out)
	if err != nil {
		return []string{watchErr(fmt.Sprintf("armed stack #%d: ", top), err)}
	}
	if failed := blocker(stack); failed != "" {
		return env.disarm(ctx, r, failed)
	}
	if len(waitingOn(stack)) > 0 {
		return nil
	}
	if err := env.flowGate(ctx, r, stack); err != nil {
		return env.disarm(ctx, r, cmp.Or(cliText(err), err.Error()))
	}
	err = env.land(ctx, r, dir, stack, &out)
	items := []string{fmt.Sprintf("armed stack #%d landing", top)}
	for line := range strings.Lines(out.String()) {
		items = append(items, strings.TrimSuffix(line, "\n"))
	}
	if err != nil {
		return append(items, env.disarm(ctx, r, cmp.Or(cliText(err), err.Error()))...)
	}
	return items
}

func (env *Env) disarm(ctx context.Context, r Record, why string) []string {
	top := r.Armed.Top
	r.Armed = nil
	r.Changed = env.Now()
	if err := env.storeRecord(ctx, r); err != nil {
		return []string{watchErr(fmt.Sprintf("disarm #%d: ", top), err)}
	}
	return []string{fmt.Sprintf("armed stack #%d disarmed: %s", top, why)}
}

func (env *Env) settleQueued(ctx context.Context, rec Record, stdout io.Writer) (bool, error) {
	queued, err := env.stackPulls(ctx, rec.Queued.PRs)
	if err != nil {
		return true, err
	}
	landed, err := env.landedEach(ctx, queued)
	if err != nil {
		return true, err
	}
	drafts, err := env.openQueueDrafts(ctx)
	if err != nil {
		return true, err
	}
	if !env.ejected(queued, landed, drafts) {
		return true, env.settle(ctx, rec, queued, landed, stdout)
	}
	_, _ = fmt.Fprintf(stdout, "#%d left the Graphite merge queue; relanding its stack\n", rec.Queued.Top)
	return false, env.unmark(ctx, rec)
}

func walkStack(open []stackPR, top int, trunk string) ([]stackPR, error) {
	byHead := map[string]stackPR{}
	var cur stackPR
	for _, p := range open {
		byHead[p.Head] = p
		if p.Number == top {
			cur = p
		}
	}
	if cur.Number == 0 {
		return nil, landErr(fmt.Sprintf("#%d is not an open PR", top))
	}
	stack := []stackPR{cur}
	for cur.Base != trunk {
		parent, ok := byHead[cur.Base]
		if !ok || len(stack) > len(open) {
			return nil, landErr(fmt.Sprintf("#%d's base %s is neither %s nor an open PR", cur.Number, cur.Base, trunk))
		}
		stack = append([]stackPR{parent}, stack...)
		cur = parent
	}
	return stack, nil
}

func (env *Env) stackOf(ctx context.Context, rec Record, top int, stdout io.Writer) ([]stackPR, string, error) {
	open, err := env.openPulls(ctx)
	if err != nil {
		return nil, "", err
	}
	walked, err := walkStack(open, top, env.Config.FeatureBranch)
	if err != nil {
		return nil, "", err
	}
	dir := env.workdir(ctx, rec, walked[len(walked)-1].Head, stdout)
	return env.graphiteStack(ctx, dir, open, walked, stdout), dir, nil
}

func (env *Env) workdir(ctx context.Context, rec Record, branch string, stdout io.Writer) string {
	if worktreeHere(rec) {
		return rec.Worktree
	}
	root := filepath.Dir(env.Common)
	dir := cmp.Or(env.checkout(ctx, root, branch), root)
	_, _ = fmt.Fprintf(stdout, "record %d's worktree %s is not on this machine; using %s\n",
		rec.Ticket, rec.Worktree, dir)
	return dir
}

func (env *Env) checkout(ctx context.Context, root, branch string) string {
	out, _ := env.Run(ctx, root, "", "git", "worktree", "list", "--porcelain")
	var path string
	for line := range strings.Lines(string(out)) {
		line = strings.TrimSuffix(line, "\n")
		if p, ok := strings.CutPrefix(line, "worktree "); ok {
			path = p
		}
		if line == "branch refs/heads/"+branch {
			return path
		}
	}
	return ""
}

func (env *Env) graphiteStack(
	ctx context.Context,
	worktree string,
	open, walked []stackPR,
	stdout io.Writer,
) []stackPR {
	out, err := env.Run(ctx, worktree, "", "gt", "log", "short", "--stack", "--reverse", "--no-interactive")
	if err != nil {
		_, _ = fmt.Fprintf(stdout, "gt log in %s failed (%v); landing the GitHub base chain\n", worktree, err)
		return walked
	}
	byHead := map[string]stackPR{}
	for _, p := range open {
		byHead[p.Head] = p
	}
	top := walked[len(walked)-1].Number
	trunk := env.Config.FeatureBranch
	prev := trunk
	var stack []stackPR
	for line := range strings.Lines(string(out)) {
		if p, ok := prOn(line, byHead); ok && (p.Base == trunk || p.Base == prev) {
			stack = append(stack, p)
			prev = p.Head
		}
		if len(stack) > 0 && stack[len(stack)-1].Number == top {
			if len(stack) > len(walked) {
				return stack
			}
			break
		}
	}
	return walked
}

func prOn(line string, byHead map[string]stackPR) (stackPR, bool) {
	for _, f := range strings.Fields(line) {
		if p, ok := byHead[f]; ok {
			return p, true
		}
	}
	return stackPR{}, false
}

func waitingOn(stack []stackPR) []string {
	var out []string
	for _, p := range stack {
		if p.Mergeable == conflicting {
			out = append(out, conflictLine(p.Number, p.Base))
			continue
		}
		t := p.flat("")
		var why []string
		if t.Stage1 != "success" {
			why = append(why, "stage 1 "+orMissing(t.Stage1))
		}
		if len(why) > 0 {
			out = append(out, fmt.Sprintf("#%d (%s)", p.Number, strings.Join(why, ", ")))
		}
	}
	return out
}

func orMissing(s string) string {
	if s == "" {
		return "missing"
	}
	return s
}

func (env *Env) land(ctx context.Context, rec Record, dir string, stack []stackPR, stdout io.Writer) error {
	nums := make([]int, len(stack))
	for i, p := range stack {
		nums[i] = p.Number
	}
	top := nums[len(nums)-1]
	if err := env.cleanRuns(ctx, stack, stdout); err != nil {
		return err
	}
	if err := env.mergeable(ctx, dir, stack[0], top); err != nil {
		return err
	}
	for _, n := range slices.Backward(nums) {
		if err := env.addLabel(ctx, n); err != nil {
			return landFailed(err)
		}
	}
	rec.Queued = &Queue{Top: top, PRs: nums, At: env.Now()}
	rec.Armed = nil
	rec.Settled = nil
	rec.Changed = env.Now()
	if err := env.storeRecord(ctx, rec); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(stdout, "queued %s\nfollow it: monacoctl agents watch (under Claude Code's Monitor tool)\n",
		prRefs(nums))
	return env.reportDraft(ctx, stack, stdout)
}

func prRefs(nums []int) string {
	refs := make([]string, len(nums))
	for i, n := range nums {
		refs[i] = "#" + strconv.Itoa(n)
	}
	return strings.Join(refs, " ")
}

func (env *Env) mergeable(ctx context.Context, worktree string, bottom stackPR, top int) error {
	trunk := env.Config.FeatureBranch
	if _, err := env.Run(ctx, worktree, "", "git", "fetch", "--quiet", "origin", trunk, bottom.HeadOID); err != nil {
		return landFailed(err)
	}
	_, err := env.Run(ctx, worktree, "", "git", "merge-tree", "--write-tree", "origin/"+trunk, bottom.HeadOID)
	var exit *exec.ExitError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &exit) && exit.ExitCode() == 1:
		return landErr(fmt.Sprintf("#%d conflicts with %s. Fix the conflicts with gt modify and "+
			"gt submit --stack --draft, then run land-stack %d", bottom.Number, trunk, top))
	default:
		return landFailed(err)
	}
}

func (env *Env) settle(ctx context.Context, rec Record, prs []stackPR, landed []bool, stdout io.Writer) error {
	top := prs[len(prs)-1]
	if slices.Contains(landed, false) {
		_, _ = fmt.Fprintf(stdout, "#%d is queued in the Graphite merge queue\n", top.Number)
		return nil
	}
	sha := shortSHA(cmp.Or(top.MergeCommit.OID, top.HeadOID))
	_, _ = fmt.Fprintf(stdout, "#%d merged as %s\n", top.Number, sha)
	return env.conclude(ctx, rec, outcomeLanded, landedLine(rec.Queued))
}

func landedLine(q *Queue) string {
	return fmt.Sprintf("stack #%d landed (%s)", q.Top, prRefs(q.PRs))
}

func (env *Env) landedEach(ctx context.Context, prs []stackPR) ([]bool, error) {
	out := make([]bool, len(prs))
	for i, p := range prs {
		landed, err := env.landed(ctx, p.closed())
		if err != nil {
			return nil, err
		}
		out[i] = landed
	}
	return out, nil
}

const (
	prQueued   = "queued"
	prWaiting  = "labeled, waiting for Graphite"
	prSettling = "label just removed, waiting for Graphite"
	prLanded   = "landed"
	prEjected  = "ejected"
)

func (env *Env) queueState(p stackPR, landed bool, drafts []queueDraft) string {
	switch {
	case landed:
		return prLanded
	case p.State != "OPEN":
		return prEjected
	case draftHolds(drafts, p.Number):
		return prQueued
	case p.labeled(env.Config.QueueLabel):
		return prWaiting
	case env.justUnlabeled(p):
		return prSettling
	default:
		return prEjected
	}
}

func (env *Env) ejected(prs []stackPR, landed []bool, drafts []queueDraft) bool {
	_, ok := env.firstEjected(prs, landed, drafts)
	return ok
}

func (env *Env) firstEjected(prs []stackPR, landed []bool, drafts []queueDraft) (stackPR, bool) {
	for i, p := range prs {
		if env.queueState(p, landed[i], drafts) == prEjected {
			return p, true
		}
	}
	return stackPR{}, false
}

func ejectedWhy(out stackPR) string {
	if out.State == "CLOSED" {
		return "was closed without landing"
	}
	return "left the Graphite merge queue"
}

func (env *Env) ejectStack(ctx context.Context, rec Record, out stackPR) (string, bool, error) {
	stop := env.requeued(rec)
	err := env.releaseQueue(ctx, rec.Queued, stop)
	if errors.Is(err, errRequeued) {
		return requeuedLine(rec.Queued.Top), true, nil
	}
	if err != nil {
		return "", false, err
	}
	if again, err := stop(ctx); err != nil || again {
		return requeuedLine(rec.Queued.Top), again, err
	}
	line := ejectedLine(rec.Queued.Top, out)
	return line, false, env.conclude(ctx, rec, outcomeEjected, line)
}

func requeuedLine(top int) string {
	return fmt.Sprintf("stack #%d was re-queued during its release; left it queued", top)
}

func ejectedLine(top int, out stackPR) string {
	return fmt.Sprintf("stack #%d ejected: #%d %s", top, out.Number, ejectedWhy(out))
}

func (env *Env) requeued(rec Record) func(context.Context) (bool, error) {
	return func(ctx context.Context) (bool, error) {
		cur, err := env.record(ctx, rec.Ticket)
		if err != nil {
			return false, err
		}
		return cur.Queued != nil && !cur.Queued.At.Equal(rec.Queued.At), nil
	}
}

func (env *Env) conclude(ctx context.Context, rec Record, outcome Outcome, detail string) error {
	q := rec.Queued
	rec.Settled = &Settlement{Top: q.Top, PRs: q.PRs, Outcome: outcome, Detail: detail, At: env.Now()}
	return env.unmark(ctx, rec)
}

func (env *Env) unmark(ctx context.Context, rec Record) error {
	rec.Queued = nil
	rec.Armed = nil
	rec.Changed = env.Now()
	return env.storeRecord(ctx, rec)
}

func (env *Env) justUnlabeled(p stackPR) bool {
	events := p.TimelineItems.Nodes
	for j := len(events) - 1; j >= 0; j-- {
		if events[j].Label.Name == env.Config.QueueLabel {
			return env.Now().Sub(events[j].CreatedAt) < settleAfter
		}
	}
	return false
}

func (env *Env) unqueueEjected(ctx context.Context, rs []Record, stdout io.Writer) error {
	drafts := sync.OnceValues(func() ([]queueDraft, error) { return env.openQueueDrafts(ctx) })
	for _, r := range rs {
		if r.Queued == nil {
			continue
		}
		open, err := drafts()
		if err != nil {
			return err
		}
		prs, err := env.stackPulls(ctx, r.Queued.PRs)
		if err != nil {
			return err
		}
		landed, err := env.landedEach(ctx, prs)
		if err != nil {
			return err
		}
		out, ok := env.firstEjected(prs, landed, open)
		if !ok {
			continue
		}
		line, requeued, err := env.ejectStack(ctx, r, out)
		if err != nil {
			return err
		}
		if requeued {
			_, _ = fmt.Fprintln(stdout, line)
			continue
		}
		_, _ = fmt.Fprintf(stdout, "unqueued: #%d; #%d left the Graphite merge queue. "+
			"Fix the stack with gt modify and gt submit --stack --draft, then run land-stack %d\n",
			r.Ticket, r.Queued.Top, r.Queued.Top)
	}
	return nil
}

func (env *Env) graphqlGH(ctx context.Context, query string, out any) error {
	if env.useREST() && !checkPageQuery(query) {
		return env.restQuery(ctx, query, out)
	}
	err := env.graphqlCLI(ctx, query, out)
	if !graphqlCLIDenied(err) {
		return err
	}
	env.markREST()
	if checkPageQuery(query) {
		return err
	}
	return env.restQuery(ctx, query, out)
}

func (env *Env) graphqlCLI(ctx context.Context, query string, out any) error {
	owner, name, _ := strings.Cut(env.Config.Repo, "/")
	raw, err := env.Run(ctx, env.Work, "", "gh", "api", "graphql",
		"-f", "query="+query, "-f", "owner="+owner, "-f", "name="+name)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, &struct {
		Data any `json:"data"`
	}{out}); err != nil {
		return fmt.Errorf("decode gh api graphql: %w", err)
	}
	return nil
}

func (env *Env) openPulls(ctx context.Context) ([]stackPR, error) {
	var data struct {
		Repository struct {
			Open struct {
				Nodes []stackPR `json:"nodes"`
			} `json:"open"`
		} `json:"repository"`
	}
	q := repoQuery + "open: pullRequests(states:OPEN,first:100){nodes{" + stackFields + "}}}}"
	if err := env.graphqlGH(ctx, q, &data); err != nil {
		return nil, err
	}
	open := data.Repository.Open.Nodes
	return open, env.readStackChecks(ctx, open)
}

func (env *Env) readStackChecks(ctx context.Context, prs []stackPR) error {
	var commits []*gqlCommit
	for i := range prs {
		commits = append(commits, prs[i].commits()...)
	}
	return env.readChecks(ctx, commits, env.graphqlGH)
}

func (env *Env) stackPulls(ctx context.Context, nums []int) ([]stackPR, error) {
	var b strings.Builder
	b.WriteString(repoQuery)
	for _, n := range nums {
		_, _ = fmt.Fprintf(&b, "p%d: pullRequest(number:%d){...pr} ", n, n)
	}
	b.WriteString("}}\nfragment pr on PullRequest{" + stackFields + "}")
	var data struct {
		Repository map[string]*stackPR `json:"repository"`
	}
	if err := env.graphqlGH(ctx, b.String(), &data); err != nil {
		return nil, err
	}
	out := make([]stackPR, len(nums))
	for i, n := range nums {
		p := data.Repository["p"+strconv.Itoa(n)]
		if p == nil {
			return nil, detailErr(errs.CodeNotFound, "monacoctl.agents.land-stack", fmt.Sprintf("#%d is not a PR", n))
		}
		out[i] = *p
	}
	return out, env.readStackChecks(ctx, out)
}

func landErr(detail string) error {
	return detailErr(errs.CodeInvalidInput, "monacoctl.agents.land-stack", detail)
}

func landFailed(err error) error {
	return fmt.Errorf("%w; the stack is not marked queued: run land-stack again, which relabels every PR", err)
}
