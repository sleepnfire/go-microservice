package gin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	os.Exit(m.Run())
}

func performRequest(engine http.Handler, method, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, nil)
	engine.ServeHTTP(w, req)
	return w
}

func newTestLogger() (*slog.Logger, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	handler := slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	return slog.New(handler), buf
}

// --- NewGinService ---

func TestNewGinService_Names(t *testing.T) {
	gs := NewGinService(LogModeGin)
	assert.Equal(t, "Public", gs.Public.Name)
	assert.Equal(t, "Internal", gs.Internal.Name)
	assert.Equal(t, "Technical", gs.Technical.Name)
}

func TestNewGinService_Ports(t *testing.T) {
	gs := NewGinService(LogModeGin)
	assert.Equal(t, ":8080", gs.Public.Port)
	assert.Equal(t, ":8081", gs.Internal.Port)
	assert.Equal(t, ":8082", gs.Technical.Port)
}

func TestNewGinService_EnginesNotNil(t *testing.T) {
	gs := NewGinService(LogModeGin)
	assert.NotNil(t, gs.Public.Engine)
	assert.NotNil(t, gs.Internal.Engine)
	assert.NotNil(t, gs.Technical.Engine)
}

func TestNewGinService_MiddlewareCount_GinMode(t *testing.T) {
	gs := NewGinService(LogModeGin)
	assert.Len(t, gs.Public.Engine.Handlers, 3)
	assert.Len(t, gs.Internal.Engine.Handlers, 3)
	assert.Len(t, gs.Technical.Engine.Handlers, 3)
}

func TestNewGinService_MiddlewareCount_SlogMode(t *testing.T) {
	gs := NewGinService(LogModeSlog)
	assert.Len(t, gs.Public.Engine.Handlers, 3)
	assert.Len(t, gs.Internal.Engine.Handlers, 3)
	assert.Len(t, gs.Technical.Engine.Handlers, 3)
}

// --- ServerTag ---

func TestServerTag_SetsContextValues(t *testing.T) {
	engine := gin.New()
	engine.Use(ServerTag("MyServer", "9090"))

	var gotName, gotPort string
	engine.GET("/ping", func(c *gin.Context) {
		gotName = c.GetString("server_tag")
		gotPort = c.GetString("server_port")
		c.Status(http.StatusOK)
	})

	performRequest(engine, "GET", "/ping")

	assert.Equal(t, "MyServer", gotName)
	assert.Equal(t, "9090", gotPort)
}

func TestServerTag_CallsNext(t *testing.T) {
	engine := gin.New()
	engine.Use(ServerTag("s", "p"))

	called := false
	engine.GET("/ping", func(c *gin.Context) {
		called = true
		c.Status(http.StatusOK)
	})

	performRequest(engine, "GET", "/ping")
	assert.True(t, called)
}

// --- GinSlogMiddleware ---

func TestGinSlogMiddleware_LogsExpectedFields(t *testing.T) {
	logger, buf := newTestLogger()
	engine := gin.New()
	engine.Use(ServerTag("Public", "8080"))
	engine.Use(GinSlogMiddleware(logger))
	engine.GET("/ping", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := performRequest(engine, "GET", "/ping")
	assert.Equal(t, http.StatusOK, w.Code)

	var entry map[string]any
	require.NoError(t, json.NewDecoder(buf).Decode(&entry))

	assert.Equal(t, "http_request", entry["msg"])
	assert.Equal(t, "http_request", entry["event"])
	assert.Equal(t, "Public", entry["server"])
	assert.Equal(t, "8080", entry["port"])
	assert.Equal(t, "GET", entry["method"])
	assert.Equal(t, "/ping", entry["path"])
	assert.EqualValues(t, http.StatusOK, entry["status"])
	assert.Contains(t, entry, "latency_ms")
	assert.Contains(t, entry, "client_ip")
}

func TestGinSlogMiddleware_UsesContextTagAndPort(t *testing.T) {
	logger, buf := newTestLogger()
	engine := gin.New()
	engine.Use(ServerTag("Internal", "8081"))
	engine.Use(GinSlogMiddleware(logger))
	engine.GET("/ping", func(c *gin.Context) { c.Status(http.StatusOK) })

	performRequest(engine, "GET", "/ping")

	var entry map[string]any
	require.NoError(t, json.NewDecoder(buf).Decode(&entry))

	assert.Equal(t, "Internal", entry["server"])
	assert.Equal(t, "8081", entry["port"])
}

func TestGinSlogMiddleware_LogsNonSuccessStatus(t *testing.T) {
	logger, buf := newTestLogger()
	engine := gin.New()
	engine.Use(ServerTag("Public", "8080"))
	engine.Use(GinSlogMiddleware(logger))

	performRequest(engine, "GET", "/missing")

	var entry map[string]any
	require.NoError(t, json.NewDecoder(buf).Decode(&entry))
	assert.EqualValues(t, http.StatusNotFound, entry["status"])
}

// --- GinRecoverySlog ---

func TestGinRecoverySlog_Returns500OnPanic(t *testing.T) {
	logger, _ := newTestLogger()
	engine := gin.New()
	engine.Use(ServerTag("Technical", "8082"))
	engine.Use(GinRecoverySlog(logger))
	engine.GET("/boom", func(c *gin.Context) { panic("test panic") })

	w := performRequest(engine, "GET", "/boom")
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestGinRecoverySlog_LogsPanicDetails(t *testing.T) {
	logger, buf := newTestLogger()
	engine := gin.New()
	engine.Use(ServerTag("Technical", "8082"))
	engine.Use(GinRecoverySlog(logger))
	engine.GET("/boom", func(c *gin.Context) { panic("test panic") })

	performRequest(engine, "GET", "/boom")

	var entry map[string]any
	require.NoError(t, json.NewDecoder(buf).Decode(&entry))

	assert.Equal(t, "ERROR", entry["level"])
	assert.Equal(t, "panic_recovered", entry["msg"])
	assert.Equal(t, "panic", entry["event"])
	assert.Equal(t, "Technical", entry["server"])
	assert.Equal(t, "8082", entry["port"])
	assert.Equal(t, "/boom", entry["path"])
	assert.NotEmpty(t, entry["error"])
}

func TestGinRecoverySlog_WithErrorPanic(t *testing.T) {
	logger, _ := newTestLogger()
	engine := gin.New()
	engine.Use(ServerTag("Technical", "8082"))
	engine.Use(GinRecoverySlog(logger))
	engine.GET("/boom", func(c *gin.Context) { panic(fmt.Errorf("error panic")) })

	w := performRequest(engine, "GET", "/boom")
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// --- CustomFormatter ---

func baseParams(keys map[any]any) gin.LogFormatterParams {
	return gin.LogFormatterParams{
		TimeStamp:  time.Now(),
		StatusCode: 200,
		Latency:    5 * time.Millisecond,
		ClientIP:   "127.0.0.1",
		Method:     "GET",
		Path:       "/health",
		Keys:       keys,
	}
}

func TestCustomFormatter_BasicStructure(t *testing.T) {
	out := CustomFormatter(baseParams(nil))

	assert.True(t, strings.HasPrefix(out, "[GIN]"))
	assert.Contains(t, out, "200")
	assert.Contains(t, out, "GET")
	assert.Contains(t, out, "/health")
	assert.True(t, strings.HasSuffix(out, "\n"))
}

func TestCustomFormatter_NilKeys(t *testing.T) {
	out := CustomFormatter(baseParams(nil))
	assert.NotContains(t, out, "Public")
	assert.NotContains(t, out, "server_tag")
}

func TestCustomFormatter_WithServerTagSuffix(t *testing.T) {
	out := CustomFormatter(baseParams(map[any]any{
		"server_tag":  "Public",
		"server_port": "8080",
	}))

	assert.Contains(t, out, "| Public")
	assert.Contains(t, out, "8080 |")
	assert.True(t, strings.HasSuffix(out, "\n"))
}

func TestCustomFormatter_WithServerTagOnly(t *testing.T) {
	out := CustomFormatter(baseParams(map[any]any{
		"server_tag": "Public",
	}))

	assert.Contains(t, out, "| Public")
	assert.NotContains(t, out, "8080")
}

func TestCustomFormatter_LongLatencyTruncated(t *testing.T) {
	params := baseParams(nil)
	params.Latency = 90*time.Second + 500*time.Millisecond
	out := CustomFormatter(params)

	assert.Contains(t, out, "1m30s")
	assert.NotContains(t, out, "500ms")
}

func TestCustomFormatter_Integration_ViaEngine(t *testing.T) {
	buf := &bytes.Buffer{}
	engine := gin.New()
	engine.Use(ServerTag("Public", "8080"))
	engine.Use(gin.LoggerWithConfig(gin.LoggerConfig{
		Formatter: CustomFormatter,
		Output:    buf,
	}))
	engine.GET("/test", func(c *gin.Context) { c.Status(http.StatusOK) })

	performRequest(engine, "GET", "/test")

	assert.Contains(t, buf.String(), "| Public")
	assert.Contains(t, buf.String(), "8080 |")
}
