package httpadapter

import (
	"context"

	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

// logRecover logs every request and turns panics into a 500 problem.
func logRecover(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w}
		defer func() {
			if v := recover(); v != nil {
				log.Error("panic", "path", r.URL.Path, "panic", v)
				if sw.status == 0 {
					writeProblemStatus(sw, r, http.StatusInternalServerError, "internal", "Lỗi máy chủ", "")
				}
			}
			log.Debug("http", "method", r.Method, "path", r.URL.Path, "status", sw.status, "took", time.Since(start))
		}()
		next.ServeHTTP(sw, r)
	})
}

// securityHeaders: everything is served from our origin; no inline script.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; frame-ancestors 'none'; base-uri 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

// withTimeout bounds the request context (spec §5: 2s for search).
func withTimeout(d time.Duration, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), d)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// ipLimiter is a per-client token bucket (spec §6: 10 rps/IP for search).
type ipLimiter struct {
	rps        rate.Limit
	burst      int
	trustProxy bool
	mu         sync.Mutex
	clients    map[string]*client
}

type client struct {
	lim  *rate.Limiter
	seen time.Time
}

func newIPLimiter(ctx context.Context, rps float64, burst int, trustProxy bool) *ipLimiter {
	l := &ipLimiter{rps: rate.Limit(rps), burst: burst, trustProxy: trustProxy, clients: map[string]*client{}}
	go func() {
		t := time.NewTicker(time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-t.C:
				l.mu.Lock()
				for ip, c := range l.clients {
					if now.Sub(c.seen) > 3*time.Minute {
						delete(l.clients, ip)
					}
				}
				l.mu.Unlock()
			}
		}
	}()
	return l
}

func (l *ipLimiter) allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	c, ok := l.clients[ip]
	if !ok {
		c = &client{lim: rate.NewLimiter(l.rps, l.burst)}
		l.clients[ip] = c
	}
	c.seen = time.Now()
	return c.lim.Allow()
}

func (l *ipLimiter) wrap(next http.Handler) http.Handler {
	if l == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !l.allow(clientIP(r, l.trustProxy)) {
			w.Header().Set("Retry-After", strconv.Itoa(max(1, int(1/float64(l.rps)))))
			writeProblemStatus(w, r, http.StatusTooManyRequests, "rate-limited", "Quá nhiều yêu cầu", "")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// clientIP is the peer address, or the first X-Forwarded-For hop when the
// server sits behind a trusted proxy.
func clientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			ip, _, _ := strings.Cut(xff, ",")
			return strings.TrimSpace(ip)
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
