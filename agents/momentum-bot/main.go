// momentum-bot is Monaco's reference trading agent: a cabal votes it in with a USDC
// budget, and it trades that budget with one rule anyone can read off the screen.
//
// It watches a few xStocks, compares each price to where it was one lookback ago, buys
// what rose past a threshold and sells what it bought once it falls past one. It runs
// as a dry run unless started with --live.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

const (
	defaultWatchCount = 5
	catalogPageSize   = 100
)

type options struct {
	api, key         string
	symbols          []string
	tradeUSD, maxUSD float64
	yes              bool
	cfg              Config
}

func main() {
	if err := run(os.Args[1:], os.Getenv, os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "momentum-bot:", err)
		os.Exit(1)
	}
}

func run(args []string, getenv func(string) string, stdin io.Reader, stdout io.Writer) error {
	opts, err := parseOptions(args, getenv)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	monaco := NewMonacoClient(opts.api, opts.key, nil)
	agent, assets, err := discover(ctx, monaco, opts.symbols, stdout)
	if err != nil {
		return err
	}

	printBanner(stdout, opts, agent, assets)
	if opts.cfg.Live && !opts.yes {
		if !confirm(stdin, stdout) {
			return errors.New("not confirmed, nothing was sent")
		}
	}

	budget := NewBudget(usdToMicros(opts.tradeUSD), usdToMicros(opts.maxUSD))
	bot := NewBot(opts.cfg, assets, monaco, monaco, budget, stdout, useColor(stdout, getenv))
	return bot.Run(ctx)
}

func parseOptions(args []string, getenv func(string) string) (options, error) {
	var opts options
	var symbols string
	var live, dryRun bool

	fs := flag.NewFlagSet("momentum-bot", flag.ContinueOnError)
	fs.BoolVar(&live, "live", false, "send real intents. Without it the bot only prints what it would do")
	fs.BoolVar(&dryRun, "dry-run", false, "print decisions without sending them (the default; cannot be combined with --live)")
	fs.BoolVar(&opts.yes, "yes", false, "skip the y/N confirmation that --live asks for")
	fs.BoolVar(&opts.cfg.Once, "once", false, "exit after the first tick that has a full lookback to decide on")
	fs.StringVar(&symbols, "symbols", "", "comma-separated xStock symbols to watch, e.g. GOOGLx,NVDAx (default: the first 5 routable assets in the cabal's catalog)")
	fs.DurationVar(&opts.cfg.Interval, "interval", 30*time.Second, "how often to sample prices")
	fs.DurationVar(&opts.cfg.Lookback, "lookback", 5*time.Minute, "momentum window: each price is compared to the price this long ago")
	fs.Float64Var(&opts.cfg.Rule.BuyPct, "buy-pct", 0.5, "buy when a symbol is up at least this many percent over the lookback")
	fs.Float64Var(&opts.cfg.Rule.SellPct, "sell-pct", 0.5, "sell what the bot bought when a symbol is down at least this many percent over the lookback")
	fs.Float64Var(&opts.tradeUSD, "trade-usd", 1, "USD per buy")
	fs.Float64Var(&opts.maxUSD, "max-spend-usd", 5, "total USD this run may spend on buys; keep it below the budget the cabal voted")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Usage: momentum-bot [flags]")
		fmt.Fprintln(fs.Output(), "\nEnvironment:")
		fmt.Fprintln(fs.Output(), "  MONACO_API          API base URL, e.g. http://127.0.0.1:8080 (required)")
		fmt.Fprintln(fs.Output(), "  MONACO_AGENT_KEY    the agent key from Group > Agent in the app (required; env only, never a flag)")
		fmt.Fprintln(fs.Output(), "\nFlags:")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	if fs.NArg() > 0 {
		return options{}, fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	if live && dryRun {
		return options{}, errors.New("--live and --dry-run are mutually exclusive")
	}
	opts.cfg.Live = live

	opts.api = strings.TrimSpace(getenv("MONACO_API"))
	opts.key = strings.TrimSpace(getenv("MONACO_AGENT_KEY"))
	for _, name := range []string{"MONACO_API", "MONACO_AGENT_KEY"} {
		if strings.TrimSpace(getenv(name)) == "" {
			return options{}, fmt.Errorf("%s is not set", name)
		}
	}

	for _, symbol := range strings.Split(symbols, ",") {
		if symbol = strings.TrimSpace(symbol); symbol != "" {
			opts.symbols = append(opts.symbols, symbol)
		}
	}

	switch {
	case opts.cfg.Interval < time.Second:
		return options{}, errors.New("--interval must be at least 1s")
	case opts.cfg.Lookback < 2*opts.cfg.Interval:
		return options{}, errors.New("--lookback must be at least twice --interval")
	case opts.cfg.Rule.BuyPct < 0 || opts.cfg.Rule.SellPct < 0:
		return options{}, errors.New("--buy-pct and --sell-pct must not be negative")
	case opts.tradeUSD <= 0:
		return options{}, errors.New("--trade-usd must be positive")
	case opts.maxUSD < opts.tradeUSD:
		return options{}, errors.New("--max-spend-usd must be at least --trade-usd")
	}
	return opts, nil
}

// discover reads the agent's cabal and budget, then resolves what to watch from the
// cabal's own catalog, so the bot only ever names symbols the server can route.
func discover(ctx context.Context, monaco *MonacoClient, symbols []string, stdout io.Writer) (AgentInfo, []Asset, error) {
	out := &printer{w: stdout}
	var agent AgentInfo
	err := retry(ctx, out, "agent", func() (err error) {
		agent, err = monaco.Agent(ctx)
		return err
	})
	if err != nil {
		return AgentInfo{}, nil, err
	}
	fetch := func(query string, limit int) (assets []Asset, err error) {
		err = retry(ctx, out, "catalog", func() (err error) {
			assets, err = monaco.Assets(ctx, query, limit)
			return err
		})
		return assets, err
	}

	if len(symbols) == 0 {
		page, err := fetch("", catalogPageSize)
		if err != nil {
			return AgentInfo{}, nil, err
		}
		var assets []Asset
		for _, asset := range page {
			if asset.Routable && len(assets) < defaultWatchCount {
				assets = append(assets, asset)
			}
		}
		if len(assets) == 0 {
			return AgentInfo{}, nil, errors.New("the cabal's catalog has no routable assets")
		}
		return agent, assets, nil
	}

	var assets []Asset
	for _, symbol := range symbols {
		page, err := fetch(symbol, 1)
		if err != nil {
			return AgentInfo{}, nil, err
		}
		if len(page) == 0 || !strings.EqualFold(page[0].Symbol, symbol) {
			return AgentInfo{}, nil, fmt.Errorf("%s is not in the cabal's catalog", symbol)
		}
		if !page[0].Routable {
			return AgentInfo{}, nil, fmt.Errorf("%s has no Jupiter route right now", symbol)
		}
		assets = append(assets, page[0])
	}
	return agent, assets, nil
}

// retry runs a startup read until it succeeds. Transient failures back off and a 429
// waits out its Retry-After. A bad key or any other 4xx does not retry.
func retry(ctx context.Context, out *printer, what string, read func() error) error {
	for failures := 0; ; {
		err := read()
		var throttled *ThrottledError
		var status *StatusError
		var wait time.Duration
		switch {
		case err == nil:
			return nil
		case errors.Is(err, ErrBadKey):
			return fmt.Errorf("%w: check MONACO_AGENT_KEY", err)
		case errors.As(err, &status) && status.Status < 500:
			return fmt.Errorf("%s: %w: check MONACO_API points at a Monaco API with the /v1/agent routes", what, err)
		case ctx.Err() != nil:
			return ctx.Err()
		case errors.As(err, &throttled):
			wait = throttled.RetryAfter
		default:
			failures++
			wait = backoff(failures)
		}
		out.warn(time.Now(), "%s unavailable (%v), retrying in %s", what, err, short(wait))
		if err := sleepCtx(ctx, wait); err != nil {
			return err
		}
	}
}

func printBanner(w io.Writer, opts options, agent AgentInfo, assets []Asset) {
	names := make([]string, len(assets))
	for i, asset := range assets {
		names[i] = asset.Symbol
	}
	mode := "DRY RUN, nothing will be sent (pass --live to trade)"
	if opts.cfg.Live {
		mode = "LIVE, intents will move the cabal's money"
	}
	fmt.Fprintf(w, "Monaco momentum bot\n")
	fmt.Fprintf(w, "  mode      %s\n", mode)
	fmt.Fprintf(w, "  api       %s\n", opts.api)
	fmt.Fprintf(w, "  cabal     %s, as agent %s (%s)\n", agent.CabalName, agent.AgentName, agent.Status)
	fmt.Fprintf(w, "  budget    $%s of $%s available\n", agent.Budget.AvailableUsd, agent.Budget.AllocationUsd)
	fmt.Fprintf(w, "  key       set (hidden)\n")
	fmt.Fprintf(w, "  watching  %s\n", strings.Join(names, ", "))
	fmt.Fprintf(w, "  rule      buy at +%.2f%%, sell at -%.2f%% over %s, sampled every %s\n",
		opts.cfg.Rule.BuyPct, opts.cfg.Rule.SellPct, short(opts.cfg.Lookback), short(opts.cfg.Interval))
	fmt.Fprintf(w, "  caps      %s per trade, %s total this run\n\n", usd(usdToMicros(opts.tradeUSD)), usd(usdToMicros(opts.maxUSD)))
}

func confirm(stdin io.Reader, stdout io.Writer) bool {
	fmt.Fprint(stdout, "Trade for real with these settings? [y/N] ")
	answer, _ := bufio.NewReader(stdin).ReadString('\n')
	fmt.Fprintln(stdout)
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return true
	}
	return false
}

// useColor turns on ANSI styling only for an interactive terminal that has not opted out.
func useColor(w io.Writer, getenv func(string) string) bool {
	if getenv("NO_COLOR") != "" {
		return false
	}
	file, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
