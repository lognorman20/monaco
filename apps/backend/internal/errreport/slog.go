package errreport

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
)

// PanicAttrKey marks a log record written by a panic recoverer. The recoverer reports the
// panic itself, with the live stack, so the log handler must not report it a second time.
const PanicAttrKey = "panic"

// errAttrKey is the attribute every money path logs its failure under.
const errAttrKey = "err"

// tagAttrKeys are the low-cardinality log attributes promoted to searchable tags.
var tagAttrKeys = map[string]bool{
	"route": true, "method": true, "status": true, "branch": true,
	"worker": true, "stage": true, "provider": true, "check": true,
}

// NewSlogHandler wraps inner so that every record at error level is also reported. The
// money paths (sweeps, swaps, cash outs, pollers) already log each failure at error level
// with its ids, which makes the log stream the one place to hook them all without
// touching their code.
func NewSlogHandler(inner slog.Handler, reporter Reporter) slog.Handler {
	if reporter == nil {
		return inner
	}
	if _, off := reporter.(Noop); off {
		return inner
	}
	return &slogHandler{inner: inner, reporter: reporter}
}

type slogHandler struct {
	inner    slog.Handler
	reporter Reporter
	attrs    []slog.Attr
	prefix   string
}

func (h *slogHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *slogHandler) Handle(ctx context.Context, record slog.Record) error {
	if record.Level >= slog.LevelError {
		h.report(ctx, record)
	}
	return h.inner.Handle(ctx, record)
}

func (h *slogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := *h
	next.inner = h.inner.WithAttrs(attrs)
	next.attrs = append(append([]slog.Attr(nil), h.attrs...), prefixed(h.prefix, attrs)...)
	return &next
}

func (h *slogHandler) WithGroup(name string) slog.Handler {
	next := *h
	next.inner = h.inner.WithGroup(name)
	if name != "" {
		next.prefix = h.prefix + name + "."
	}
	return &next
}

func prefixed(prefix string, attrs []slog.Attr) []slog.Attr {
	if prefix == "" {
		return attrs
	}
	out := make([]slog.Attr, len(attrs))
	for i, attr := range attrs {
		out[i] = slog.Attr{Key: prefix + attr.Key, Value: attr.Value}
	}
	return out
}

func (h *slogHandler) report(ctx context.Context, record slog.Record) {
	event := Event{
		Level:   LevelError,
		Message: record.Message,
		Tags:    map[string]string{},
		Extra:   map[string]any{},
	}
	recovered := false
	add := func(attr slog.Attr) {
		value := attr.Value.Resolve()
		switch {
		case attr.Key == PanicAttrKey:
			recovered = true
		case attr.Key == errAttrKey:
			if err, ok := value.Any().(error); ok {
				event.Err = err
			} else {
				event.Err = errors.New(value.String())
			}
		case tagAttrKeys[attr.Key]:
			event.Tags[attr.Key] = strings.TrimSpace(fmt.Sprint(value.Any()))
		default:
			event.Extra[attr.Key] = value.Any()
		}
	}
	for _, attr := range h.attrs {
		add(attr)
	}
	record.Attrs(func(attr slog.Attr) bool {
		attr.Key = h.prefix + attr.Key
		add(attr)
		return true
	})
	if recovered {
		return
	}
	h.reporter.Report(ctx, event)
}
