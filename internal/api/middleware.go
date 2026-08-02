package api

import (
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// Middleware je standardní func(http.Handler) http.Handler.
type Middleware func(http.Handler) http.Handler

// Chain složí middleware v pořadí, ve kterém jsou uvedené — první v seznamu
// obalí ostatní zvenčí, tedy spustí se jako první na requestu a poslední
// při odpovědi.
func Chain(h http.Handler, mws ...Middleware) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}

// --- Logging ----------------------------------------------------------------

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// LoggingMiddleware zaloguje metodu, cestu, stavový kód, dobu trvání a
// klientskou adresu pro každý request. V produkci nahraďte log.Printf
// strukturovaným logerem (slog/zap) a napojte na tracing span, aby šlo
// jeden request sledovat přes parser → rules → executor.
func LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(rec, r)

		log.Printf("%s %s %d %s client=%s",
			r.Method, r.URL.Path, rec.status, time.Since(start), clientIP(r))
	})
}

func clientIP(r *http.Request) string {
	// Za reverzní proxy/load balancerem nastavte důvěryhodně X-Forwarded-For
	// (nebo X-Real-IP) až na hraně, jinak si klient může IP pro rate limiting podvrhnout.
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		return fwd
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// --- Rate limiting ------------------------------------------------------------

// RateLimiter poskytuje per-klient token bucket rate limiting. Klíčem je
// zde IP adresa; u autentizovaného API dává větší smysl klíčovat podle
// API klíče nebo tenant ID.
type RateLimiter struct {
	mu       sync.Mutex
	limiters map[string]*rate.Limiter
	seen     map[string]time.Time
	rps      rate.Limit
	burst    int
	ttl      time.Duration
}

// NewRateLimiter vytvoří limiter s danou propustností (požadavků/s) a burstem.
// ttl určuje, jak dlouho se nečinný per-klient limiter drží v paměti, než se
// uvolní — brání neomezenému růstu mapy při velkém počtu unikátních klientů.
func NewRateLimiter(rps float64, burst int, ttl time.Duration) *RateLimiter {
	if rps <= 0 {
		rps = 5
	}
	if burst <= 0 {
		burst = 10
	}
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}

	rl := &RateLimiter{
		limiters: make(map[string]*rate.Limiter),
		seen:     make(map[string]time.Time),
		rps:      rate.Limit(rps),
		burst:    burst,
		ttl:      ttl,
	}
	go rl.cleanupLoop()
	return rl
}

func (rl *RateLimiter) getLimiter(key string) *rate.Limiter {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	lim, ok := rl.limiters[key]
	if !ok {
		lim = rate.NewLimiter(rl.rps, rl.burst)
		rl.limiters[key] = lim
	}
	rl.seen[key] = time.Now()
	return lim
}

func (rl *RateLimiter) cleanupLoop() {
	ticker := time.NewTicker(rl.ttl)
	defer ticker.Stop()
	for range ticker.C {
		cutoff := time.Now().Add(-rl.ttl)
		rl.mu.Lock()
		for key, last := range rl.seen {
			if last.Before(cutoff) {
				delete(rl.seen, key)
				delete(rl.limiters, key)
			}
		}
		rl.mu.Unlock()
	}
}

// Middleware vrátí http middleware, který každý request omezí podle klienta.
// Při vyčerpání limitu vrací 429 Too Many Requests s hlavičkou Retry-After.
func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := clientIP(r)
		if !rl.getLimiter(key).Allow() {
			w.Header().Set("Retry-After", "1")
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}
