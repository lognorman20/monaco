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
	api, groupID, key string
	priceURL          string
	symbols           []string
	tradeUSD, maxUSD  float64
	yes               bool
	cfg               Config
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

	monaco := NewMonacoClient(opts.api, opts.groupID, opts.key, nil)
	assets, err := discover(ctx, monaco, opts.symbols, stdout)
	if err != nil {
		return err
	}

	printBanner(stdout, opts, assets)
	if opts.cfg.Live && !opts.yes {
		if !confirm(stdin, stdout) {
			return errors.New("not confirmed, nothing was sent")
		}
	}

	budget := NewBudget(usdToMicros(opts.tradeUSD), usdToMicros(opts.maxUSD))
	bot := NewBot(opts.cfg, assets, monaco, NewPriceClient(opts.priceURL, nil), budget, stdout, useColor(stdout, getenv))
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
		fmt.Fprintln(fs.Output(), "  MONACO_GROUP_ID     the cabal's group id (required)")
		fmt.Fprintln(fs.Output(), "  MONACO_AGENT_KEY    the key shown once in the app after the add-bot vote (required; env only, never a flag)")
		fmt.Fprintln(fs.Output(), "  JUPITER_PRICE_URL   override the price feed (default "+defaultPriceURL+")")
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
	opts.groupID = strings.TrimSpace(getenv("MONACO_GROUP_ID"))
	opts.key = strings.TrimSpace(getenv("MONACO_AGENT_KEY"))
	opts.priceURL = strings.TrimSpace(getenv("JUPITER_PRICE_URL"))
	for _, name := range []string{"MONACO_API", "MONACO_GROUP_ID", "MONACO_AGENT_KEY"} {
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

// discover resolves what to watch from the cabal's own catalog, so the bot only ever
// names symbols the server can route. Transient failures back off; a bad key does not retry.
func discover(ctx context.Context, monaco *MonacoClient, symbols []string, stdout io.Writer) ([]Asset, error) {
	out := &printer{w: stdout}
	fetch := func(query string, limit int) ([]Asset, error) {
		for failures := 0; ; {
			assets, err := monaco.Assets(ctx, query, limit)
			var throttled *ThrottledError
			var wait time.Duration
			switch {
			case err == nil:
				return assets, nil
			case errors.Is(err, ErrBadKey):
				return nil, fmt.Errorf("%w: check MONACO_AGENT_KEY and MONACO_GROUP_ID", err)
			case ctx.Err() != nil:
				return nil, ctx.Err()
			case errors.As(err, &throttled):
				wait = throttled.RetryAfter
			default:
				failures++
				wait = backoff(failures)
			}
			out.warn(time.Now(), "catalog unavailable (%v), retrying in %s", err, short(wait))
			if err := sleepCtx(ctx, wait); err != nil {
				return nil, err
			}
		}
	}

	if len(symbols) == 0 {
		page, err := fetch("", catalogPageSize)
		if err != nil {
			return nil, err
		}
		var assets []Asset
		for _, asset := range page {
			if asset.Routable && asset.SolanaMint != "" && len(assets) < defaultWatchCount {
				assets = append(assets, asset)
			}
		}
		if len(assets) == 0 {
			return nil, errors.New("the cabal's catalog has no routable assets")
		}
		return assets, nil
	}

	var assets []Asset
	for _, symbol := range symbols {
		page, err := fetch(symbol, 1)
		if err != nil {
			return nil, err
		}
		if len(page) == 0 || !strings.EqualFold(page[0].Symbol, symbol) || page[0].SolanaMint == "" {
			return nil, fmt.Errorf("%s is not in the cabal's catalog", symbol)
		}
		if !page[0].Routable {
			return nil, fmt.Errorf("%s has no Jupiter route right now", symbol)
		}
		assets = append(assets, page[0])
	}
	return assets, nil
}

func printBanner(w io.Writer, opts options, assets []Asset) {
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
	fmt.Fprintf(w, "  cabal     %s\n", opts.groupID)
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
