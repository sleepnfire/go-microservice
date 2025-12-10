package gin

import (
	"fmt"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
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

func NewGinService() GinService {
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

	gs.Public.Engine.Use(ServerTag(Public, Port80[1:]))
	gs.Internal.Engine.Use(ServerTag(Internal, Port81[1:]))
	gs.Technical.Engine.Use(ServerTag(Technical, Port82[1:]))

	gs.Public.Engine.Use(gin.LoggerWithFormatter(CustomFormatter))
	gs.Public.Engine.Use(gin.Recovery())

	gs.Internal.Engine.Use(gin.LoggerWithFormatter(CustomFormatter))
	gs.Internal.Engine.Use(gin.Recovery())

	gs.Technical.Engine.Use(gin.LoggerWithFormatter(CustomFormatter))
	gs.Technical.Engine.Use(gin.Recovery())

	return gs
}
