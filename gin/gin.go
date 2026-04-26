package gin

import (
	"crypto/rand"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type LogMode int

const (
	LogModeGin  LogMode = iota
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
	publicRoutes    func(gin.IRouter)
	internalRoutes  func(gin.IRouter)
	technicalRoutes func(gin.IRouter)
}

// Option configures a GinService.
type Option func(*serviceConfig)

func WithPublicPort(port string) Option {
	return func(c *serviceConfig) { c.publicPort = port }
}

func WithInternalPort(port string) Option {
	return func(c *serviceConfig) { c.internalPort = port }
}

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

func generateRequestID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// RequestID reads X-Request-ID from the incoming request (or generates one)
// and writes it back in the response header and gin context.
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader("X-Request-ID")
		if id == "" {
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
	return func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("X-XSS-Protection", "0")
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Next()
	}
}

// MaxBodySize limits the request body to limit bytes on the Public server.
func MaxBodySize(limit int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
		c.Next()
	}
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
			"query", c.Request.URL.RawQuery,
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

		logger.Error("panic_recovered",
			"event", "panic",
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

func registerTechnicalRoutes(api *ApiGin, cfg *serviceConfig) {
	api.Engine.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	api.Engine.GET("/ready", func(c *gin.Context) {
		if cfg.readinessCheck != nil && !cfg.readinessCheck() {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "not_ready"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ready"})
	})

	if cfg.version != "" {
		v := cfg.version
		api.Engine.GET("/version", func(c *gin.Context) {
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
		Public:    ApiGin{Engine: gin.New(), Name: Public, Port: cfg.publicPort},
		Internal:  ApiGin{Engine: gin.New(), Name: Internal, Port: cfg.internalPort},
		Technical: ApiGin{Engine: gin.New(), Name: Technical, Port: cfg.technicalPort},
	}

	setup := func(api *ApiGin) {
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
		gs.Public.Engine.Use(SecureHeaders())
	}
	if cfg.maxBodySize > 0 {
		gs.Public.Engine.Use(MaxBodySize(cfg.maxBodySize))
	}

	// Built-in Technical routes, then optional consumer callbacks.
	registerTechnicalRoutes(&gs.Technical, cfg)

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
