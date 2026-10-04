package api

import (
	"crypto/rand"
	"encoding/hex"
	"log"
	"net"
	"net/http"
	"strings"
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
func LoggingMiddleware(clientIP KeyFunc) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

			next.ServeHTTP(rec, r)

			log.Printf("%s %s %d %s client=%s request_id=%s",
				r.Method, r.URL.Path, rec.status, time.Since(start), clientIP(r), w.Header().Get("X-Request-ID"))
		})
	}
}

// RequestIDMiddleware převezme X-Request-ID od klienta (pokud je rozumně
// krátké), jinak vygeneruje nové, a vrátí ho v odpovědi — pro dohledání
// requestu v logu.
func RequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" || len(id) > 64 {
			var b [8]byte
			_, _ = rand.Read(b[:])
			id = hex.EncodeToString(b[:])
		}
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r)
	})
}

// RecoverMiddleware zachytí panic v handleru a vrátí 500 místo pádu spojení.
func RecoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				log.Printf("panic: %v (%s %s)", v, r.Method, r.URL.Path)
				writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// KeyFunc určí klíč klienta (typicky IP) pro rate limiting a logování.
type KeyFunc func(*http.Request) string

// ClientIP vrátí KeyFunc pro určení IP klienta. S trustProxy=true čte
// X-Forwarded-For (první, tj. původní adresu) a X-Real-IP — zapínejte jen
// za důvěryhodnou reverzní proxy, jinak si klient může IP podvrhnout
// a obejít rate limiting.
func ClientIP(trustProxy bool) KeyFunc {
	return func(r *http.Request) string {
		if trustProxy {
			if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
				first, _, _ := strings.Cut(fwd, ",")
				if ip := strings.TrimSpace(first); ip != "" {
					return ip
				}
			}
			if real := strings.TrimSpace(r.Header.Get("X-Real-IP")); real != "" {
				return real
			}
		}
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			return r.RemoteAddr
		}
		return host
	}
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
	key      KeyFunc
	stop     chan struct{}
	once     sync.Once
}

// NewRateLimiter vytvoří limiter s danou propustností (požadavků/s) a burstem.
// ttl určuje, jak dlouho se nečinný per-klient limiter drží v paměti, než se
// uvolní — brání neomezenému růstu mapy při velkém počtu unikátních klientů.
func NewRateLimiter(rps float64, burst int, ttl time.Duration, key KeyFunc) *RateLimiter {
	if key == nil {
		key = ClientIP(false)
	}
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
		key:      key,
		stop:     make(chan struct{}),
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
	for {
		select {
		case <-rl.stop:
			return
		case <-ticker.C:
		}
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

// Close zastaví úklidovou goroutinu.
func (rl *RateLimiter) Close() {
	rl.once.Do(func() { close(rl.stop) })
}

// Middleware vrátí http middleware, který každý request omezí podle klienta.
// Při vyčerpání limitu vrací 429 Too Many Requests s hlavičkou Retry-After.
func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !rl.getLimiter(rl.key(r)).Allow() {
			w.Header().Set("Retry-After", "1")
			writeError(w, http.StatusTooManyRequests, "rate_limited", "rate limit exceeded")
			return
		}
		next.ServeHTTP(w, r)
	})
}
