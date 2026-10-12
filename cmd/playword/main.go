// Command playword is the web server: JSON API and HTMX interface over the
// in-memory index, rebuilt whenever ingest or rebuild-keys notifies.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"playword/internal/adapters/http"
	"playword/internal/adapters/index/rhymeindex"
	"playword/internal/adapters/oauth"
	"playword/internal/adapters/observability"
	"playword/internal/adapters/postgres"
	"playword/internal/adapters/sources/registry"
	"playword/internal/app"
	"playword/internal/domain/account"
	"playword/internal/domain/entry"
	"playword/internal/domain/rhyme"
	"playword/internal/domain/rhyme/en"
	"playword/internal/ingest"
	"playword/internal/ports"
)

// version is set at build time: -ldflags "-X main.version=…".
var version = "dev"

// config is read from the environment (12-factor) and validated at start.
type config struct {
	Addr         string
	MetricsAddr  string // Prometheus /metrics, kept off the public port; "" disables
	DatabaseURL  string
	ConfigDir    string
	RateLimitRPS float64
	RateBurst    int
	TrustProxy   bool
	CacheSize    int
	LogLevel     slog.Level
	Insecure     bool   // cookies without the Secure flag (plain-HTTP development only)
	IPSalt       string // salt for the IP hashes stored with sessions and events
	AdminToken   string // bearer token for the admin API (spec §6, first phase)
	Analytics    bool   // record usage events (M7)
	Schedule     map[string]time.Duration
	PublicURL    string // https://playword.vn: base of OAuth redirect URLs
	GoogleID     string // Google sign-in; both empty disables it
	GoogleSecret string
	Contact      string // contact email on the legal pages
}

func loadConfig() (config, error) {
	c := config{Addr: ":8020", MetricsAddr: ":9090", ConfigDir: "configs", RateLimitRPS: 10, RateBurst: 20, CacheSize: 10000, Analytics: true}
	var errs []error
	str := func(key string, dst *string) {
		if v, ok := os.LookupEnv(key); ok {
			*dst = v
		}
	}
	num := func(key string, parse func(string) error) {
		if v, ok := os.LookupEnv(key); ok {
			if err := parse(v); err != nil {
				errs = append(errs, fmt.Errorf("%s=%q: %w", key, v, err))
			}
		}
	}
	str("PLAYWORD_ADDR", &c.Addr)
	str("PLAYWORD_METRICS_ADDR", &c.MetricsAddr)
	str("DATABASE_URL", &c.DatabaseURL)
	str("PLAYWORD_CONFIG_DIR", &c.ConfigDir)
	num("PLAYWORD_RATE_LIMIT_RPS", func(v string) (err error) { c.RateLimitRPS, err = strconv.ParseFloat(v, 64); return })
	num("PLAYWORD_RATE_LIMIT_BURST", func(v string) (err error) { c.RateBurst, err = strconv.Atoi(v); return })
	num("PLAYWORD_TRUST_PROXY", func(v string) (err error) { c.TrustProxy, err = strconv.ParseBool(v); return })
	num("PLAYWORD_CACHE_SIZE", func(v string) (err error) { c.CacheSize, err = strconv.Atoi(v); return })
	num("PLAYWORD_LOG_LEVEL", func(v string) error { return c.LogLevel.UnmarshalText([]byte(v)) })
	num("PLAYWORD_INSECURE_COOKIES", func(v string) (err error) { c.Insecure, err = strconv.ParseBool(v); return })
	str("PLAYWORD_IP_SALT", &c.IPSalt)
	str("PLAYWORD_ADMIN_TOKEN", &c.AdminToken)
	num("PLAYWORD_ANALYTICS", func(v string) (err error) { c.Analytics, err = strconv.ParseBool(v); return })
	num("PLAYWORD_INGEST_SCHEDULE", func(v string) (err error) { c.Schedule, err = ingest.ParseSchedule(v); return })
	str("PLAYWORD_PUBLIC_URL", &c.PublicURL)
	str("PLAYWORD_CONTACT_EMAIL", &c.Contact)
	str("PLAYWORD_GOOGLE_CLIENT_ID", &c.GoogleID)
	str("PLAYWORD_GOOGLE_CLIENT_SECRET", &c.GoogleSecret)
	c.PublicURL = strings.TrimRight(c.PublicURL, "/")
	if (c.GoogleID == "") != (c.GoogleSecret == "") {
		errs = append(errs, errors.New("set both PLAYWORD_GOOGLE_CLIENT_ID and PLAYWORD_GOOGLE_CLIENT_SECRET, or neither"))
	}
	if c.GoogleID != "" || c.PublicURL != "" {
		if u, err := url.Parse(c.PublicURL); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.Path != "" {
			errs = append(errs, errors.New("PLAYWORD_PUBLIC_URL must be like https://playword.vn (scheme and host only); Google sign-in needs it"))
		} else if u.Scheme == "http" && !c.Insecure {
			errs = append(errs, errors.New("PLAYWORD_PUBLIC_URL is http: use https, or PLAYWORD_INSECURE_COOKIES=true for local development"))
		}
	}
	if c.AdminToken != "" && len(c.AdminToken) < 32 {
		errs = append(errs, errors.New("PLAYWORD_ADMIN_TOKEN must have at least 32 characters"))
	}
	if c.DatabaseURL == "" {
		errs = append(errs, errors.New("DATABASE_URL is required"))
	}
	if c.RateLimitRPS < 0 || c.RateBurst < 1 || c.CacheSize < 1 {
		errs = append(errs, errors.New("rate limit, burst and cache size must be positive"))
	}
	return c, errors.Join(errs...)
}

func main() {
	if err := run(); err != nil {
		slog.Error("playword", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(log)

	// Take the ports before anything else starts, so a busy port fails fast
	// with one clear error instead of after background jobs began.
	ln, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return fmt.Errorf("%w — is another playword running? (change with PLAYWORD_ADDR)", err)
	}
	defer func() { _ = ln.Close() }()
	var metricsLn net.Listener
	if cfg.MetricsAddr != "" {
		if metricsLn, err = net.Listen("tcp", cfg.MetricsAddr); err != nil {
			return fmt.Errorf("%w — change PLAYWORD_METRICS_ADDR, or set it empty to disable /metrics", err)
		}
		defer func() { _ = metricsLn.Close() }()
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	shutdownTracing, err := observability.SetupTracing(ctx, "playword", version)
	if err != nil {
		return fmt.Errorf("tracing: %w", err)
	}
	defer func() { _ = shutdownTracing(context.Background()) }()

	engine, err := rhyme.LoadDir(cfg.ConfigDir)
	if err != nil {
		return err
	}
	st, err := postgres.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer st.Close()

	var holder rhymeindex.Holder
	var metrics *observability.Metrics
	var lastErr atomic.Pointer[error]
	notLoaded := errors.New("index not loaded yet")
	lastErr.Store(&notLoaded)
	build := func() {
		start := time.Now()
		snap, err := holder.Rebuild(ctx, st, engine)
		if err != nil {
			if holder.Load() == nil {
				lastErr.Store(&err)
			}
			log.Error("index build failed", "err", err)
			return
		}
		lastErr.Store(nil)
		if metrics != nil {
			metrics.ObserveBuild(time.Since(start))
		}
		log.Info("index ready", "gen", snap.Gen, "entries", snap.Len(), "rhymable", snap.Rhymable(), "took", time.Since(start))
	}
	go func() {
		build()
		wait := st.WaitForChangeOrPoll(60 * time.Second)
		for ctx.Err() == nil {
			if err := wait(ctx); err != nil {
				if ctx.Err() == nil {
					log.Error("index watch", "err", err)
					time.Sleep(5 * time.Second)
				}
				continue
			}
			build()
		}
	}()

	if cfg.IPSalt == "" {
		cfg.IPSalt = account.NewToken()
		log.Warn("PLAYWORD_IP_SALT not set: IP hashes will not match across restarts")
	}
	svc := app.NewService(&holder, engine, cfg.CacheSize)
	pipeline := ingest.Pipeline{Store: st, Engine: engine, Log: log}
	ingester := app.NewIngester(st, pipeline, registry.Dictionaries())
	if len(cfg.Schedule) > 0 {
		ingest.Schedule(ctx, cfg.Schedule, func(ctx context.Context, name string) error {
			rep, err := ingester.Run(ctx, name, false)
			if errors.Is(err, app.ErrBusy) {
				return nil // another server is on it
			}
			if err == nil {
				log.Info("scheduled ingest", "source", name, "status", rep.Status, "inserted", rep.Inserted, "updated", rep.Updated)
			}
			return err
		}, log)
		log.Info("ingest schedule", "sources", len(cfg.Schedule))
	}
	slangSvc := app.NewSlang(st, pipeline, func(ctx context.Context) (*en.Pronouncer, error) {
		prons, err := st.Pronunciations(ctx, entry.EN)
		return en.NewPronouncer(prons), err
	})
	ingester.WithSlang(slangSvc)
	var recorder *app.Recorder
	if cfg.Analytics {
		recorder = app.NewRecorder(st, 10_000, log)
		go recorder.Run(ctx)
	}
	dash := app.NewDashboard(st, st, recorder)
	every(ctx, log, st, "analytics:refresh", 10*time.Minute, dash.Maintain)
	every(ctx, log, st, "analytics:autobump", time.Hour, func(ctx context.Context) error {
		// Weekly, however often servers restart: the run is claimed in the DB.
		if due, err := st.ClaimJob(ctx, "analytics:autobump", 7*24*time.Hour); err != nil || !due {
			return err
		}
		n, err := dash.AutoBump(ctx)
		if n > 0 {
			log.Info("slang trend auto-bump", "entries", n)
		}
		return err
	})
	every(ctx, log, st, "sessions:cleanup", 24*time.Hour, func(ctx context.Context) error {
		_, err := st.DeleteExpiredSessions(ctx)
		return err
	})
	providers := map[string]ports.OAuthProvider{}
	if cfg.GoogleID != "" {
		g := oauth.NewGoogle(cfg.GoogleID, cfg.GoogleSecret, cfg.PublicURL+"/auth/google/callback")
		providers[g.Name()] = g
		log.Info("google sign-in enabled", "redirect", cfg.PublicURL+"/auth/google/callback")
	}
	web := httpadapter.New(ctx, svc, httpadapter.Options{
		Recorder: recorder, Dashboard: dash, OAuth: providers,
		PublicURL: cfg.PublicURL, Contact: cfg.Contact, Version: version,
		Slang: slangSvc, Ingest: ingester, AdminToken: cfg.AdminToken, SubmitRPM: 1,
		Log: log, RateLimitRPS: cfg.RateLimitRPS, RateBurst: cfg.RateBurst, TrustProxy: cfg.TrustProxy,
		Accounts: app.NewAccounts(st, cfg.IPSalt), SecureCookies: !cfg.Insecure, LoginRPM: 5,
		Ready: func() error {
			if e := lastErr.Load(); e != nil {
				return *e
			}
			return nil
		},
	})
	snapStat := func(f func(*rhymeindex.Snapshot) float64) func() float64 {
		return func() float64 {
			if s := holder.Load(); s != nil {
				return f(s)
			}
			return 0
		}
	}
	metrics = observability.New(observability.Sources{
		IndexEntries:   snapStat(func(s *rhymeindex.Snapshot) float64 { return float64(s.Len()) }),
		IndexRhymable:  snapStat(func(s *rhymeindex.Snapshot) float64 { return float64(s.Rhymable()) }),
		IndexGen:       snapStat(func(s *rhymeindex.Snapshot) float64 { return float64(s.Gen) }),
		CacheHits:      func() float64 { h, _ := svc.CacheStats(); return float64(h) },
		CacheMisses:    func() float64 { _, m := svc.CacheStats(); return float64(m) },
		EventsRecorded: func() float64 { return float64(recorder.Recorded()) },
		EventsDropped:  func() float64 { return float64(recorder.Dropped()) },
		EventsFailed:   func() float64 { return float64(recorder.Failed()) },
	})
	if metricsLn != nil {
		ms := &http.Server{Handler: metrics.Handler(), ReadHeaderTimeout: 5 * time.Second}
		go func() {
			if err := ms.Serve(metricsLn); !errors.Is(err, http.ErrServerClosed) {
				log.Error("metrics server", "err", err)
			}
		}()
		defer func() { _ = ms.Close() }()
	}
	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           observability.Trace(metrics.Instrument(web.Handler())),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	log.Info("listening", "addr", cfg.Addr, "metrics", cfg.MetricsAddr, "version", version, "rhyme_config", engine.ConfigID()[:12])

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return srv.Shutdown(shutdown)
}

// every runs job now and then every interval, under a cluster-wide lock so
// only one server does it.
func every(ctx context.Context, log *slog.Logger, st *postgres.Store, name string, interval time.Duration, job func(context.Context) error) {
	run := func() {
		if _, err := st.TryLock(ctx, name, func() error { return job(ctx) }); err != nil && ctx.Err() == nil {
			log.Error("background job", "job", name, "err", err)
		}
	}
	go func() {
		run()
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				run()
			}
		}
	}()
}
