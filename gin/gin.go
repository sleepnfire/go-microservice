package gin

import (
	"crypto/rand"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
)

type LogMode int

const (
	LogModeGin LogMode = iota
	LogModeSlog
	LogModeNoop
)

const (
	Public    = "Public"
	Internal  = "Internal"
	Technical = "Technical"

	Port80 = ":8080"
	Port81 = ":8081"
	Port82 = ":8082"
)

type ApiGin struct {
	Engine *gin.Engine
	Name   string
	Port   string
}

type GinService struct {
	Public    ApiGin
	Internal  ApiGin
	Technical ApiGin

	// shuttingDown makes GET /ready return 503 once the service starts stopping.
	shuttingDown *atomic.Bool
}

// SetShuttingDown marks the service as stopping: GET /ready then returns 503 so
// that load balancers drain traffic before the servers are shut down.
// It is called automatically by MicroService.Stop.
func (gs GinService) SetShuttingDown() {
	if gs.shuttingDown != nil {
		gs.shuttingDown.Store(true)
	}
}

// serviceConfig holds the configuration built by Option functions.
type serviceConfig struct {
	publicPort      string
	internalPort    string
	technicalPort   string
	readinessCheck  func() bool
	version         string
	secureHeaders   bool
	maxBodySize     int64
	hstsMaxAge      time.Duration
	trustedProxies  []string
	publicRoutes    func(gin.IRouter)
	internalRoutes  func(gin.IRouter)
	technicalRoutes func(gin.IRouter)
}

// Option configures a GinService.
type Option func(*serviceConfig)

// WithPublicPort sets the Public listen address (default ":8080").
// Use "127.0.0.1:8080" to listen on loopback only.
func WithPublicPort(port string) Option {
	return func(c *serviceConfig) { c.publicPort = port }
}

// WithInternalPort sets the Internal listen address (default ":8081").
// Prefer "127.0.0.1:8081" when only local callers need it.
func WithInternalPort(port string) Option {
	return func(c *serviceConfig) { c.internalPort = port }
}

// WithTechnicalPort sets the Technical listen address (default ":8082").
// Prefer "127.0.0.1:8082" when only local callers need it.
func WithTechnicalPort(port string) Option {
	return func(c *serviceConfig) { c.technicalPort = port }
}

// WithReadinessCheck registers a function called on GET /ready.
// Returns 503 when the function returns false, 200 otherwise.
func WithReadinessCheck(fn func() bool) Option {
	return func(c *serviceConfig) { c.readinessCheck = fn }
}

// WithVersion exposes GET /version on the Technical server.
func WithVersion(v string) Option {
	return func(c *serviceConfig) { c.version = v }
}

// WithSecureHeaders controls whether security headers are added to Public responses.
// Enabled by default. Disable only if you manage headers yourself.
func WithSecureHeaders(enabled bool) Option {
	return func(c *serviceConfig) { c.secureHeaders = enabled }
}

// WithMaxBodySize limits the request body size on the Public server.
// 0 (default) means no limit. Example: WithMaxBodySize(4 << 20) for 4 MB.
func WithMaxBodySize(limit int64) Option {
	return func(c *serviceConfig) { c.maxBodySize = limit }
}

// WithHSTS adds a Strict-Transport-Security header (RFC 6797) on Public responses.
// Only enable it when the service is reached exclusively over HTTPS
// (e.g. behind a TLS-terminating proxy). 0 (default) disables the header.
func WithHSTS(maxAge time.Duration) Option {
	return func(c *serviceConfig) { c.hstsMaxAge = maxAge }
}

// WithTrustedProxies declares the IPs/CIDRs of reverse proxies allowed to set
// X-Forwarded-For / X-Real-IP. By default no proxy is trusted, so
// gin.Context.ClientIP() returns the TCP peer address and cannot be spoofed.
// Applied to the three servers. An invalid IP/CIDR panics at construction.
func WithTrustedProxies(cidrs ...string) Option {
	return func(c *serviceConfig) { c.trustedProxies = cidrs }
}

// WithPublicRoutes registers routes on the Public engine at construction time.
func WithPublicRoutes(fn func(gin.IRouter)) Option {
	return func(c *serviceConfig) { c.publicRoutes = fn }
}

// WithInternalRoutes registers routes on the Internal engine at construction time.
func WithInternalRoutes(fn func(gin.IRouter)) Option {
	return func(c *serviceConfig) { c.internalRoutes = fn }
}

// WithTechnicalRoutes registers additional routes on the Technical engine.
// The built-in /health, /ready, and /version routes are always registered first.
func WithTechnicalRoutes(fn func(gin.IRouter)) Option {
	return func(c *serviceConfig) { c.technicalRoutes = fn }
}

func NewJSONLogger() *slog.Logger {
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})
	return slog.New(handler)
}

// generateRequestID returns a random UUID version 4 (RFC 9562 §5.4).
func generateRequestID() string {
	b := make([]byte, 16)
	rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10 (RFC 9562)
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

const maxRequestIDLength = 128

// validRequestID reports whether an incoming X-Request-ID is safe to propagate
// (bounded length, restricted charset) — prevents log and header injection.
func validRequestID(id string) bool {
	if id == "" || len(id) > maxRequestIDLength {
		return false
	}
	for i := 0; i < len(id); i++ {
		ch := id[i]
		switch {
		case ch >= 'a' && ch <= 'z', ch >= 'A' && ch <= 'Z', ch >= '0' && ch <= '9',
			ch == '-', ch == '_', ch == '.':
		default:
			return false
		}
	}
	return true
}

// RequestID reads X-Request-ID from the incoming request (or generates one)
// and writes it back in the response header and gin context.
// Incoming IDs longer than 128 chars or outside [A-Za-z0-9._-] are replaced.
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader("X-Request-ID")
		if !validRequestID(id) {
			id = generateRequestID()
		}
		c.Set("request_id", id)
		c.Header("X-Request-ID", id)
		c.Next()
	}
}

// SecureHeaders adds defensive HTTP response headers to every response.
// Applied on the Public server only.
func SecureHeaders() gin.HandlerFunc {
	return SecureHeadersWithHSTS(0)
}

// SecureHeadersWithHSTS is SecureHeaders plus Strict-Transport-Security
// (RFC 6797) when hstsMaxAge > 0.
func SecureHeadersWithHSTS(hstsMaxAge time.Duration) gin.HandlerFunc {
	hsts := ""
	if hstsMaxAge > 0 {
		hsts = "max-age=" + strconv.FormatInt(int64(hstsMaxAge/time.Second), 10) + "; includeSubDomains"
	}
	return func(c *gin.Context) {
		h := c.Writer.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Content-Security-Policy", "frame-ancestors 'none'")
		h.Set("X-XSS-Protection", "0")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		if hsts != "" {
			h.Set("Strict-Transport-Security", hsts)
		}
		c.Next()
	}
}

// MaxBodySize limits the request body to limit bytes on the Public server.
// Requests announcing a larger Content-Length are rejected immediately with
// 413 Content Too Large (RFC 9110 §15.5.14); chunked bodies are capped by
// http.MaxBytesReader and fail on read.
func MaxBodySize(limit int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.ContentLength > limit {
			c.Header("Connection", "close")
			c.AbortWithStatus(http.StatusRequestEntityTooLarge)
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
		c.Next()
	}
}

// sensitiveQueryKeys are query parameters whose values are redacted in logs.
var sensitiveQueryKeys = map[string]struct{}{
	"token": {}, "access_token": {}, "refresh_token": {}, "id_token": {},
	"code": {}, "state": {}, "password": {}, "passwd": {}, "secret": {},
	"client_secret": {}, "api_key": {}, "apikey": {}, "key": {}, "session": {},
}

// redactQuery masks the values of sensitive query parameters before logging.
func redactQuery(raw string) string {
	if raw == "" {
		return ""
	}
	values, err := url.ParseQuery(raw)
	if err != nil {
		return "[unparseable]"
	}
	redacted := false
	for k := range values {
		if _, ok := sensitiveQueryKeys[strings.ToLower(k)]; ok {
			for i := range values[k] {
				values[k][i] = "REDACTED"
			}
			redacted = true
		}
	}
	if !redacted {
		return raw
	}
	return values.Encode()
}

func GinSlogMiddleware(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()

		c.Next()

		latency := time.Since(start)

		serverName, _ := c.Get("server_tag")
		serverPort, _ := c.Get("server_port")
		requestID, _ := c.Get("request_id")

		logger.Info("http_request",
			"event", "http_request",
			"request_id", requestID,
			"server", serverName,
			"port", serverPort,
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"query", redactQuery(c.Request.URL.RawQuery),
			"status", c.Writer.Status(),
			"latency_ms", latency.Milliseconds(),
			"bytes", c.Writer.Size(),
			"client_ip", c.ClientIP(),
			"user_agent", c.Request.UserAgent(),
		)
	}
}

func GinRecoverySlog(logger *slog.Logger) gin.HandlerFunc {
	return gin.CustomRecovery(func(c *gin.Context, recovered any) {
		serverName, _ := c.Get("server_tag")
		serverPort, _ := c.Get("server_port")
		requestID, _ := c.Get("request_id")

		logger.Error("panic_recovered",
			"event", "panic",
			"request_id", requestID,
			"server", serverName,
			"port", serverPort,
			"path", c.Request.URL.Path,
			"error", recovered,
		)

		c.AbortWithStatus(500)
	})
}

func ServerTag(name string, port string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("server_tag", name)
		c.Set("server_port", port)
		c.Next()
	}
}

// Reuse of gin -> logger.go file
func CustomFormatter(param gin.LogFormatterParams) string {
	var statusColor, methodColor, resetColor string
	if param.IsOutputColor() {
		statusColor = param.StatusCodeColor()
		methodColor = param.MethodColor()
		resetColor = param.ResetColor()
	}

	if param.Latency > time.Minute {
		param.Latency = param.Latency.Truncate(time.Second)
	}

	suffix := ""
	if param.Keys != nil {
		// server_tag
		if v, ok := param.Keys["server_tag"]; ok && v != nil {
			suffix = fmt.Sprintf(" | %v", v)
		}
		// server_port
		if v, ok := param.Keys["server_port"]; ok && v != nil {
			s := fmt.Sprintf("%v", v)
			if s != "" {
				suffix = suffix + fmt.Sprintf(" %s |", s)
			}
		}
	}

	base := fmt.Sprintf("[GIN] %v |%s %3d %s| %13v | %15s |%s %-7s %s %#v\n%s",
		param.TimeStamp.Format("2006/01/02 - 15:04:05"),
		statusColor, param.StatusCode, resetColor,
		param.Latency,
		param.ClientIP,
		methodColor, param.Method, resetColor,
		param.Path,
		param.ErrorMessage,
	)

	// delete the final '\n' and add it on the suffix part '\n'
	return strings.TrimRight(base, "\n") + suffix + "\n"
}

// getAndHead registers h for both GET and HEAD (RFC 9110 §9.1).
func getAndHead(r gin.IRouter, path string, h gin.HandlerFunc) {
	r.GET(path, h)
	r.HEAD(path, h)
}

func registerTechnicalRoutes(api *ApiGin, cfg *serviceConfig, shuttingDown *atomic.Bool) {
	noStore := func(c *gin.Context) { c.Header("Cache-Control", "no-store") }

	getAndHead(api.Engine, "/health", func(c *gin.Context) {
		noStore(c)
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	getAndHead(api.Engine, "/ready", func(c *gin.Context) {
		noStore(c)
		if shuttingDown.Load() || (cfg.readinessCheck != nil && !cfg.readinessCheck()) {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "not_ready"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ready"})
	})

	if cfg.version != "" {
		v := cfg.version
		getAndHead(api.Engine, "/version", func(c *gin.Context) {
			noStore(c)
			c.JSON(http.StatusOK, gin.H{"version": v})
		})
	}
}

func NewGinService(mode LogMode, opts ...Option) GinService {
	cfg := &serviceConfig{
		publicPort:    Port80,
		internalPort:  Port81,
		technicalPort: Port82,
		secureHeaders: true,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	// Use release mode unless test mode is already active.
	if gin.Mode() == gin.DebugMode {
		gin.SetMode(gin.ReleaseMode)
	}

	var slogger *slog.Logger
	if mode == LogModeSlog {
		slogger = NewJSONLogger()
	}

	gs := GinService{
		Public:       ApiGin{Engine: gin.New(), Name: Public, Port: cfg.publicPort},
		Internal:     ApiGin{Engine: gin.New(), Name: Internal, Port: cfg.internalPort},
		Technical:    ApiGin{Engine: gin.New(), Name: Technical, Port: cfg.technicalPort},
		shuttingDown: &atomic.Bool{},
	}

	setup := func(api *ApiGin) {
		// Trust no proxy unless explicitly configured: ClientIP() then cannot be
		// spoofed through X-Forwarded-For.
		if err := api.Engine.SetTrustedProxies(cfg.trustedProxies); err != nil {
			panic(fmt.Sprintf("go-microservice: invalid trusted proxy: %v", err))
		}
		// Answer 405 + Allow instead of 404 for a known path (RFC 9110 §15.5.6).
		api.Engine.HandleMethodNotAllowed = true

		portNum := strings.TrimPrefix(api.Port, ":")
		api.Engine.Use(ServerTag(api.Name, portNum))
		api.Engine.Use(RequestID())

		switch mode {
		case LogModeGin:
			api.Engine.Use(gin.LoggerWithFormatter(CustomFormatter))
			api.Engine.Use(gin.Recovery())
		case LogModeSlog:
			api.Engine.Use(GinRecoverySlog(slogger))
			api.Engine.Use(GinSlogMiddleware(slogger))
		case LogModeNoop:
			api.Engine.Use(gin.Recovery())
		}
	}

	setup(&gs.Public)
	setup(&gs.Internal)
	setup(&gs.Technical)

	// Public-only middleware: security headers and body size limit.
	if cfg.secureHeaders {
		gs.Public.Engine.Use(SecureHeadersWithHSTS(cfg.hstsMaxAge))
	}
	if cfg.maxBodySize > 0 {
		gs.Public.Engine.Use(MaxBodySize(cfg.maxBodySize))
	}

	// Built-in Technical routes, then optional consumer callbacks.
	registerTechnicalRoutes(&gs.Technical, cfg, gs.shuttingDown)

	if cfg.publicRoutes != nil {
		cfg.publicRoutes(gs.Public.Engine)
	}
	if cfg.internalRoutes != nil {
		cfg.internalRoutes(gs.Internal.Engine)
	}
	if cfg.technicalRoutes != nil {
		cfg.technicalRoutes(gs.Technical.Engine)
	}

	return gs
}
