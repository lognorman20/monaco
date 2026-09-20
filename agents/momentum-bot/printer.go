package main

import (
	"fmt"
	"io"
	"time"
)

// printer writes the decision log. It is the bot's only output besides fatal errors.
type printer struct {
	w     io.Writer
	color bool
}

const (
	ansiDim    = "\x1b[2m"
	ansiGreen  = "\x1b[32m"
	ansiYellow = "\x1b[33m"
	ansiBold   = "\x1b[1m"
	ansiReset  = "\x1b[0m"
)

func (p *printer) line(at time.Time, style, text string) {
	stamp := at.Format("15:04:05")
	if p.color {
		fmt.Fprintf(p.w, "%s%s%s  %s%s%s\n", ansiDim, stamp, ansiReset, style, text, ansiReset)
		return
	}
	fmt.Fprintf(p.w, "%s  %s\n", stamp, text)
}

func (p *printer) symbol(at time.Time, symbol string, price float64, note string) {
	quote := "       —"
	if price > 0 {
		quote = fmt.Sprintf("$%7.2f", price)
	}
	p.line(at, "", fmt.Sprintf("%-7s %s  %s", symbol, quote, note))
}

func (p *printer) action(at time.Time, format string, args ...any) {
	p.line(at, ansiBold, fmt.Sprintf(format, args...))
}

func (p *printer) ok(at time.Time, format string, args ...any) {
	p.line(at, ansiGreen, fmt.Sprintf(format, args...))
}

func (p *printer) warn(at time.Time, format string, args ...any) {
	p.line(at, ansiYellow, fmt.Sprintf(format, args...))
}

func usd(micros int64) string { return fmt.Sprintf("$%.2f", float64(micros)/usdcMicrosPerUSD) }

func shares(atoms int64) string { return fmt.Sprintf("%.6f", float64(atoms)/tokenAtomsPerShare) }

// short renders a duration without the trailing zero units of Duration.String ("5m", not "5m0s").
func short(d time.Duration) string {
	d = d.Round(time.Second)
	if d == 0 {
		return "0s"
	}
	out := ""
	if h := d / time.Hour; h > 0 {
		out += fmt.Sprintf("%dh", h)
		d -= h * time.Hour
	}
	if m := d / time.Minute; m > 0 {
		out += fmt.Sprintf("%dm", m)
		d -= m * time.Minute
	}
	if s := d / time.Second; s > 0 {
		out += fmt.Sprintf("%ds", s)
	}
	return out
}
