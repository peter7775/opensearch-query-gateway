package api

import "net/http"

// NewRouter sestaví HTTP routy služby. Logging je zapnutý globálně, rate
// limiting jen na /v1/search — health check endpoint zůstává neomezený,
// aby ho throttling nezasahoval (load balancery/orchestrátory ho volají
// často a nezávisle na provozu API). Pro bohatší routing nebo další
// middleware (auth, tracing) vyměňte http.ServeMux za chi/gin.
func NewRouter(h *SearchHandler, limiter *RateLimiter) http.Handler {
	mux := http.NewServeMux()

	searchHandler := Chain(http.HandlerFunc(h.Handle), limiter.Middleware)
	mux.Handle("/v1/search", searchHandler)

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	return LoggingMiddleware(mux)
}
