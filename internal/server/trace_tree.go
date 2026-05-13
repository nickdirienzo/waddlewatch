package server

import (
	"sort"
	"time"

	"github.com/nickdirienzo/waddlewatch/internal/query"
)

// traceNode is one row in the rendered span tree.
type traceNode struct {
	Span      query.TraceSpan
	Depth     int
	OffsetPct float64
	WidthPct  float64
	IndentPx  int
}

// traceTree converts a flat list of spans into a depth-ordered slice with
// offsets sized against the trace's overall start..end window.
func traceTree(spans []query.TraceSpan) ([]traceNode, time.Time, time.Time) {
	if len(spans) == 0 {
		return nil, time.Time{}, time.Time{}
	}

	traceStart := spans[0].StartTime
	traceEnd := spans[0].EndTime
	for _, s := range spans {
		if s.StartTime.Before(traceStart) {
			traceStart = s.StartTime
		}
		if s.EndTime.After(traceEnd) {
			traceEnd = s.EndTime
		}
	}
	total := traceEnd.Sub(traceStart)
	if total <= 0 {
		total = time.Millisecond
	}

	known := make(map[string]bool, len(spans))
	for _, s := range spans {
		if s.SpanID != "" {
			known[s.SpanID] = true
		}
	}

	children := map[string][]query.TraceSpan{}
	var roots []query.TraceSpan
	for _, s := range spans {
		if s.ParentSpanID == "" || !known[s.ParentSpanID] {
			roots = append(roots, s)
			continue
		}
		children[s.ParentSpanID] = append(children[s.ParentSpanID], s)
	}

	startBefore := func(a, b query.TraceSpan) bool { return a.StartTime.Before(b.StartTime) }
	sort.Slice(roots, func(i, j int) bool { return startBefore(roots[i], roots[j]) })
	for k := range children {
		c := children[k]
		sort.Slice(c, func(i, j int) bool { return startBefore(c[i], c[j]) })
		children[k] = c
	}

	var out []traceNode
	var visit func(s query.TraceSpan, depth int)
	visit = func(s query.TraceSpan, depth int) {
		offset := float64(s.StartTime.Sub(traceStart)) / float64(total) * 100
		width := float64(s.EndTime.Sub(s.StartTime)) / float64(total) * 100
		if width < 0.3 {
			width = 0.3
		}
		out = append(out, traceNode{
			Span: s, Depth: depth,
			OffsetPct: offset, WidthPct: width,
			IndentPx: 12 * depth,
		})
		for _, c := range children[s.SpanID] {
			visit(c, depth+1)
		}
	}
	for _, r := range roots {
		visit(r, 0)
	}
	return out, traceStart, traceEnd
}
