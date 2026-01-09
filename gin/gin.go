package gin

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type LogMode int

const (
	LogModeGin LogMode = iota
	LogModeSlog
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

func NewJSONLogger() *slog.Logger {
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})
	return slog.New(handler)
}

func GinSlogMiddleware(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()

		c.Next()

		latency := time.Since(start)

		serverName, _ := c.Get("server_tag")
		serverPort, _ := c.Get("server_port")

		logger.Info("http_request",
			"event", "http_request",
			"server", serverName,
			"port", serverPort,
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status", c.Writer.Status(),
			"latency_ms", latency.Milliseconds(),
			"client_ip", c.ClientIP(),
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

func NewGinService(mode LogMode) GinService {
	var slogger *slog.Logger
	if mode == LogModeSlog {
		slogger = NewJSONLogger()
	}

	gs := GinService{
		Public: ApiGin{
			Engine: gin.New(),
			Name:   Public,
			Port:   Port80,
		},
		Internal: ApiGin{
			Engine: gin.New(),
			Name:   Internal,
			Port:   Port81,
		},
		Technical: ApiGin{
			Engine: gin.New(),
			Name:   Technical,
			Port:   Port82,
		},
	}

	setup := func(api *ApiGin, name, port string) {
		api.Engine.Use(ServerTag(name, port))

		if mode == LogModeGin {
			api.Engine.Use(gin.LoggerWithFormatter(CustomFormatter))
			api.Engine.Use(gin.Recovery())
		}

		if mode == LogModeSlog {
			api.Engine.Use(GinRecoverySlog(slogger))
			api.Engine.Use(GinSlogMiddleware(slogger))
		}
	}

	setup(&gs.Public, Public, Port80[1:])
	setup(&gs.Internal, Internal, Port81[1:])
	setup(&gs.Technical, Technical, Port82[1:])

	return gs
}
