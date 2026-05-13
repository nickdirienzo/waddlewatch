// Package server hosts the htmx-driven UI and SSE log tail.
package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/nickdirienzo/waddlewatch/internal/catalog"
	"github.com/nickdirienzo/waddlewatch/internal/query"
)

// Deps wires the runtime collaborators a Server needs.
type Deps struct {
	Store       *catalog.Store
	Templates   fs.FS
	Static      fs.FS
	Logger      *slog.Logger
	StoragePath string
	CatalogPath string
}

// Server holds parsed templates and serves the UI.
type Server struct {
	deps      Deps
	templates *templateSet
}

// New parses templates and returns a server ready to mount.
func New(d Deps) (*Server, error) {
	if d.Store == nil {
		return nil, errors.New("server: store is required")
	}
	if d.Templates == nil {
		return nil, errors.New("server: templates fs is required")
	}
	if d.Static == nil {
		return nil, errors.New("server: static fs is required")
	}
	if d.Logger == nil {
		d.Logger = slog.Default()
	}
	tpl, err := parseTemplates(d.Templates)
	if err != nil {
		return nil, err
	}
	return &Server{deps: d, templates: tpl}, nil
}

// Routes builds the chi router.
func (s *Server) Routes() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(s.requestLogger)
	r.Use(middleware.Recoverer)

	r.Get("/healthz", s.handleHealth)
	r.Get("/", s.handleIndex)

	r.Get("/logs", s.handleLogsPage)
	r.Get("/logs/rows", s.handleLogsRows)

	r.Get("/metrics", s.handleMetricsPage)
	r.Get("/metrics/chart", s.handleMetricsChart)

	r.Get("/traces", s.handleTracesPage)
	r.Get("/traces/rows", s.handleTracesRows)

	r.Get("/tail", s.handleTailPage)
	r.Get("/tail/stream", s.handleTailStream)

	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(s.deps.Static))))
	return r
}

func (s *Server) requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		s.deps.Logger.Info("http",
			"method", r.Method,
			"path", r.URL.Path,
			"status", ww.Status(),
			"bytes", ww.BytesWritten(),
			"duration_ms", time.Since(start).Milliseconds(),
		)
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	data := map[string]any{
		"Title":       "Home",
		"Page":        "home",
		"NeedsChart":  false,
		"StoragePath": s.deps.StoragePath,
		"CatalogPath": s.deps.CatalogPath,
	}
	s.render(w, r, "index", data)
}

func (s *Server) handleLogsPage(w http.ResponseWriter, r *http.Request) {
	services, _ := query.Services(r.Context(), s.deps.Store.DB(), "otel_logs")
	q := r.URL.Query()
	data := map[string]any{
		"Title":      "Logs",
		"Page":       "logs",
		"NeedsChart": false,
		"From":       q.Get("from"),
		"To":         q.Get("to"),
		"Service":    q.Get("service"),
		"Search":     q.Get("search"),
		"Services":   services,
		"Query":      encodeQuery(q),
	}
	s.render(w, r, "logs", data)
}

func (s *Server) handleLogsRows(w http.ResponseWriter, r *http.Request) {
	rng, err := query.ParseRange(r.URL.Query().Get("from"), r.URL.Query().Get("to"))
	if err != nil {
		s.renderPartial(w, r, "logs_rows", map[string]any{"Error": err.Error()})
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	rows, err := query.Logs(r.Context(), s.deps.Store.DB(), query.LogsQuery{
		Range:   rng,
		Service: r.URL.Query().Get("service"),
		Search:  r.URL.Query().Get("search"),
		Limit:   limit,
	})
	if err != nil {
		s.renderPartial(w, r, "logs_rows", map[string]any{"Error": err.Error()})
		return
	}
	s.renderPartial(w, r, "logs_rows", map[string]any{"Rows": rows})
}

func (s *Server) handleMetricsPage(w http.ResponseWriter, r *http.Request) {
	services, _ := query.Services(r.Context(), s.deps.Store.DB(), "otel_metrics")
	q := r.URL.Query()
	bucket := q.Get("bucket")
	if bucket == "" {
		bucket = "60"
	}
	data := map[string]any{
		"Title":      "Metrics",
		"Page":       "metrics",
		"NeedsChart": true,
		"From":       q.Get("from"),
		"To":         q.Get("to"),
		"Service":    q.Get("service"),
		"MetricName": q.Get("name"),
		"BucketSecs": bucket,
		"Services":   services,
		"Query":      encodeQuery(q),
	}
	s.render(w, r, "metrics", data)
}

func (s *Server) handleMetricsChart(w http.ResponseWriter, r *http.Request) {
	rng, err := query.ParseRange(r.URL.Query().Get("from"), r.URL.Query().Get("to"))
	if err != nil {
		s.renderPartial(w, r, "metrics_chart", map[string]any{"Error": err.Error()})
		return
	}
	bucketSecs, _ := strconv.Atoi(r.URL.Query().Get("bucket"))
	if bucketSecs <= 0 {
		bucketSecs = 60
	}
	rows, err := query.Metrics(r.Context(), s.deps.Store.DB(), query.MetricsQuery{
		Range:   rng,
		Service: r.URL.Query().Get("service"),
		Name:    r.URL.Query().Get("name"),
		Bucket:  time.Duration(bucketSecs) * time.Second,
	})
	if err != nil {
		s.renderPartial(w, r, "metrics_chart", map[string]any{"Error": err.Error()})
		return
	}

	type series struct {
		Name   string    `json:"name"`
		Values []float64 `json:"values"`
	}
	labelIdx := map[time.Time]int{}
	labels := []time.Time{}
	seriesByName := map[string]*series{}
	for _, r := range rows {
		if _, ok := labelIdx[r.Bucket]; !ok {
			labelIdx[r.Bucket] = len(labels)
			labels = append(labels, r.Bucket)
		}
	}
	for _, r := range rows {
		key := r.Service + "/" + r.Name
		ss, ok := seriesByName[key]
		if !ok {
			ss = &series{Name: key, Values: make([]float64, len(labels))}
			seriesByName[key] = ss
		}
		ss.Values[labelIdx[r.Bucket]] = r.Value
	}

	labelStrs := make([]string, len(labels))
	for i, t := range labels {
		labelStrs[i] = t.Format("15:04")
	}
	out := struct {
		Labels []string  `json:"labels"`
		Series []*series `json:"series"`
	}{Labels: labelStrs}
	for _, ss := range seriesByName {
		out.Series = append(out.Series, ss)
	}
	payload, err := json.Marshal(out)
	if err != nil {
		s.renderPartial(w, r, "metrics_chart", map[string]any{"Error": err.Error()})
		return
	}
	s.renderPartial(w, r, "metrics_chart", map[string]any{
		"Series":     out.Series,
		"PointCount": len(rows),
		"JSON":       template.JS(payload),
	})
}

func (s *Server) handleTracesPage(w http.ResponseWriter, r *http.Request) {
	services, _ := query.Services(r.Context(), s.deps.Store.DB(), "otel_traces")
	q := r.URL.Query()
	data := map[string]any{
		"Title":      "Traces",
		"Page":       "traces",
		"NeedsChart": false,
		"From":       q.Get("from"),
		"To":         q.Get("to"),
		"Service":    q.Get("service"),
		"TraceID":    q.Get("trace_id"),
		"Services":   services,
		"Query":      encodeQuery(q),
	}
	s.render(w, r, "traces", data)
}

func (s *Server) handleTracesRows(w http.ResponseWriter, r *http.Request) {
	rng, err := query.ParseRange(r.URL.Query().Get("from"), r.URL.Query().Get("to"))
	if err != nil {
		s.renderPartial(w, r, "traces_rows", map[string]any{"Error": err.Error()})
		return
	}
	rows, err := query.Traces(r.Context(), s.deps.Store.DB(), query.TracesQuery{
		Range:   rng,
		Service: r.URL.Query().Get("service"),
		TraceID: r.URL.Query().Get("trace_id"),
	})
	if err != nil {
		s.renderPartial(w, r, "traces_rows", map[string]any{"Error": err.Error()})
		return
	}
	s.renderPartial(w, r, "traces_rows", map[string]any{"Rows": rows})
}

func (s *Server) handleTailPage(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, "tail", map[string]any{
		"Title":      "Live tail",
		"Page":       "tail",
		"NeedsChart": false,
	})
}

func (s *Server) handleTailStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	since := time.Now().UTC().Add(-5 * time.Minute)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			now := time.Now().UTC()
			rows, err := query.Logs(r.Context(), s.deps.Store.DB(), query.LogsQuery{
				Range: query.Range{Start: since, End: now},
				Limit: 200,
			})
			if err != nil {
				fmt.Fprintf(w, "event: error\ndata: %s\n\n", htmlEscape(err.Error()))
				flusher.Flush()
				continue
			}
			for i := len(rows) - 1; i >= 0; i-- {
				row := rows[i]
				line := fmt.Sprintf(
					`<div class="tail-line"><span class="code">%s</span> <span class="sev-%s">%s</span> %s <span>%s</span></div>`,
					row.Timestamp.Format("15:04:05.000"),
					htmlEscape(row.Severity),
					htmlEscape(row.Severity),
					htmlEscape(row.Service),
					htmlEscape(row.Body),
				)
				fmt.Fprintf(w, "event: log\ndata: %s\n\n", line)
			}
			flusher.Flush()
			since = now
		}
	}
}

// render writes a full page using layout.html + the named content template.
func (s *Server) render(w http.ResponseWriter, r *http.Request, page string, data any) {
	tpl, ok := s.templates.pages[page]
	if !ok {
		s.deps.Logger.Error("unknown page template", "page", page)
		http.Error(w, "template missing", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tpl.ExecuteTemplate(w, "layout", data); err != nil {
		s.deps.Logger.Error("rendering page", "page", page, "error", err)
	}
}

// renderPartial writes a fragment for htmx swaps.
func (s *Server) renderPartial(w http.ResponseWriter, r *http.Request, name string, data any) {
	tpl, ok := s.templates.partials[name]
	if !ok {
		http.Error(w, "partial missing", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tpl.ExecuteTemplate(w, name, data); err != nil {
		s.deps.Logger.Error("rendering partial", "name", name, "error", err)
	}
}

func encodeQuery(v url.Values) string {
	if len(v) == 0 {
		return ""
	}
	return "?" + v.Encode()
}

func htmlEscape(s string) string { return template.HTMLEscapeString(s) }

// Start runs the HTTP server until ctx is cancelled.
func (s *Server) Start(ctx context.Context, addr string) error {
	srv := &http.Server{
		Addr:         addr,
		Handler:      s.Routes(),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 0, // SSE streams must not be cut.
		IdleTimeout:  120 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		s.deps.Logger.Info("http listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}

// unused import guard for sql.
var _ = sql.ErrNoRows
