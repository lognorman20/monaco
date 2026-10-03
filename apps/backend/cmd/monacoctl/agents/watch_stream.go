package agents

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"
)

const (
	watchUse     = "watch [--once] [--every <duration of 10s or more>]"
	defaultEvery = 30 * time.Second
	minEvery     = 10 * time.Second
	droppedWhy   = "dropped from the Graphite merge queue"
)

func watchArgs(args []string) (bool, time.Duration, error) {
	once, every := false, defaultEvery
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--once":
			once = true
		case args[i] == "--every" && i+1 < len(args):
			d, err := time.ParseDuration(args[i+1])
			if err != nil || d < minEvery {
				return false, 0, usageError(watchUse)
			}
			every = d
			i++
		default:
			return false, 0, usageError(watchUse)
		}
	}
	return once, every, nil
}

func streams(args []string) bool {
	return args[0] == "check" || args[0] == "watch" && !slices.Contains(args[1:], "--once")
}

type stream struct {
	env      *Env
	since    time.Time
	prev     map[string]bool
	ejected  map[int]bool
	blocks   map[string]string
	seenOpen map[int]bool
	reported map[int]bool
}

func newStream(env *Env) *stream {
	return &stream{
		env: env, since: env.Now(), prev: map[string]bool{}, ejected: map[int]bool{}, blocks: map[string]string{},
		seenOpen: map[int]bool{}, reported: map[int]bool{},
	}
}

func (env *Env) watchStream(ctx context.Context, every time.Duration, out io.Writer) error {
	s := newStream(env)
	for {
		for _, item := range s.next(ctx) {
			_, _ = io.WriteString(out, item+"\n")
		}
		select {
		case <-ctx.Done():
			return nil
		case <-env.After(every):
		}
	}
}

func (s *stream) next(ctx context.Context) []string {
	s.env.trunk = nil
	seen := map[string]bool{}
	var fresh []string
	items, every := s.round(ctx)
	for _, item := range items {
		if !s.prev[item] && !seen[item] {
			fresh = append(fresh, item)
		}
		seen[item] = true
	}
	s.prev = seen
	return append(fresh, every...)
}

func (s *stream) round(ctx context.Context) ([]string, []string) {
	env := s.env
	rs, err := env.records()
	if err != nil {
		return []string{watchErr("", err)}, nil
	}
	items, _, err := env.ownerLines(ctx, rs)
	if err != nil {
		items = append(items, watchErr("", err))
	}
	data, err := env.watchData(ctx)
	if err != nil {
		items = append(items, watchErr("", err))
	}
	var queued []int
	for _, r := range rs {
		switch {
		case r.Queued != nil:
			queued = append(queued, r.Queued.PRs...)
			items = append(items, s.stack(ctx, r, data.drafts)...)
		case r.Armed != nil:
			items = append(items, env.landArmed(ctx, r)...)
		}
		if r.Settled != nil && r.Settled.At.After(s.since) {
			items = append(items, r.Settled.Detail)
		}
	}
	items = append(items, s.draftLines(data.drafts, queued)...)
	every := append(stuckOnGraphiteBase(data.prs, rs, env.Config.QueueLabel), env.silentStalls(ctx, data.prs, rs)...)
	return append(items, s.failures(ctx, data, queued)...), every
}

func (s *stream) stack(ctx context.Context, r Record, drafts []queueDraft) []string {
	env, top := s.env, r.Queued.Top
	prs, err := env.stackPulls(ctx, r.Queued.PRs)
	if err != nil {
		return []string{watchErr(fmt.Sprintf("stack #%d: ", top), err)}
	}
	each, err := env.landedEach(ctx, prs)
	if err != nil {
		return []string{watchErr(fmt.Sprintf("stack #%d: ", top), err)}
	}
	var items []string
	landed, out := 0, stackPR{}
	for i, p := range prs {
		state := env.queueState(p, each[i], drafts)
		line := fmt.Sprintf("#%d %s", p.Number, state)
		if state == prWaiting {
			line += fmt.Sprintf(" (stage 1 %s)", orMissing(p.flat("").Stage1))
		}
		items = append(items, line)
		switch state {
		case prLanded:
			landed++
		case prEjected:
			if out.Number == 0 {
				out = p
			}
		}
	}
	wasOut := s.ejected[r.Ticket]
	delete(s.ejected, r.Ticket)
	switch {
	case landed == len(prs):
		if err := env.settle(ctx, r, prs, each, io.Discard); err != nil {
			return append(items, watchErr(fmt.Sprintf("settle #%d: ", top), err))
		}
		return append(items, landedLine(r.Queued))
	case out.Number == 0:
		return items
	case !wasOut:
		s.ejected[r.Ticket] = true
		return items
	}
	return append(items, s.eject(ctx, r, prs, out, drafts)...)
}

func (s *stream) eject(ctx context.Context, r Record, prs []stackPR, out stackPR, drafts []queueDraft) []string {
	env, top := s.env, r.Queued.Top
	held := false
	for _, p := range prs {
		if p.labeled(env.Config.QueueLabel) {
			if err := env.removeLabel(ctx, p.Number); err != nil {
				return []string{watchErr(fmt.Sprintf("eject #%d: ", top), err)}
			}
		}
		held = held || draftHolds(drafts, p.Number)
	}
	if held {
		s.ejected[r.Ticket] = true
		return nil
	}
	again, err := env.requeued(r)(ctx)
	if err != nil {
		return []string{watchErr(fmt.Sprintf("eject #%d: ", top), err)}
	}
	if again {
		return []string{requeuedLine(top)}
	}
	line := ejectedLine(top, out)
	if err := env.conclude(ctx, r, outcomeEjected, line); err != nil {
		return []string{watchErr(fmt.Sprintf("eject #%d: ", top), err)}
	}
	s.reported[out.Number] = true
	f := failure{
		PR: out.Number, Head: out.HeadOID, Body: out.Body, Why: ejectedWhy(out),
		Job: queueJob(out.Number, out.Commits, drafts, time.Time{}),
	}
	return []string{line, s.block(ctx, f)}
}

func (s *stream) draftLines(drafts []queueDraft, queued []int) []string {
	var items []string
	for _, d := range drafts {
		switch {
		case !slices.ContainsFunc(queued, d.tests):
			continue
		case d.State != "OPEN":
			if s.seenOpen[d.Number] {
				items = append(items, fmt.Sprintf("draft #%d closed", d.Number))
			}
			continue
		}
		s.seenOpen[d.Number] = true
		items = append(items, fmt.Sprintf("draft #%d open", d.Number))
		for _, c := range d.Commits.Nodes {
			for _, x := range c.Commit.latest() {
				if verdict, done := finished(x); done {
					items = append(items, fmt.Sprintf("draft #%d %s: %s", d.Number, cmp.Or(x.Name, x.Context), verdict))
				}
			}
		}
	}
	return items
}

func finished(x gqlContext) (string, bool) {
	switch {
	case x.Context != "" && x.State == "SUCCESS":
		return "pass", true
	case x.Context != "" && (x.State == "FAILURE" || x.State == "ERROR"):
		return "fail", true
	case x.Context != "" || x.Conclusion == "":
		return "", false
	case x.Conclusion == "NEUTRAL" || x.Conclusion == "SKIPPED":
		return "", false
	case x.Conclusion == "SUCCESS":
		return "pass", true
	default:
		return "fail", true
	}
}

func (s *stream) failures(ctx context.Context, data watchData, queued []int) []string {
	env := s.env
	labeled := map[int]bool{}
	for _, p := range data.prs {
		labeled[p.Number] = slices.Contains(p.Labels.Nodes, gqlName{env.Config.QueueLabel})
	}
	queue := queueRuns{label: env.Config.QueueLabel, drafts: data.drafts}
	var items []string
	for _, f := range failures(data.prs, queue, env.Config.FeatureBranch, s.since) {
		if f.Why == droppedWhy && (labeled[f.PR] || s.reported[f.PR] || slices.Contains(queued, f.PR)) {
			continue
		}
		items = append(items, s.block(ctx, f))
	}
	return items
}

func (s *stream) block(ctx context.Context, f failure) string {
	key := fmt.Sprint(f.PR, f.Why, f.Head, f.Job.DatabaseID)
	if b, ok := s.blocks[key]; ok {
		return b
	}
	b := strings.TrimSuffix(s.env.freshOwnerFor(ctx, f), "\n")
	s.blocks[key] = b
	return b
}

func watchErr(what string, err error) string {
	return "watch error: " + what + cmp.Or(cliText(err), err.Error())
}
