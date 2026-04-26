package gin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
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

func performRequestWithHeaders(engine http.Handler, method, path string, headers map[string]string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
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
	// Public: ServerTag + RequestID + Logger + Recovery + SecureHeaders
	assert.Len(t, gs.Public.Engine.Handlers, 5)
	// Internal/Technical: ServerTag + RequestID + Logger + Recovery
	assert.Len(t, gs.Internal.Engine.Handlers, 4)
	assert.Len(t, gs.Technical.Engine.Handlers, 4)
}

func TestNewGinService_MiddlewareCount_SlogMode(t *testing.T) {
	gs := NewGinService(LogModeSlog)
	// Public: ServerTag + RequestID + RecoverySlog + SlogMiddleware + SecureHeaders
	assert.Len(t, gs.Public.Engine.Handlers, 5)
	// Internal/Technical: ServerTag + RequestID + RecoverySlog + SlogMiddleware
	assert.Len(t, gs.Internal.Engine.Handlers, 4)
	assert.Len(t, gs.Technical.Engine.Handlers, 4)
}

func TestNewGinService_MiddlewareCount_NoopMode(t *testing.T) {
	gs := NewGinService(LogModeNoop)
	// Public: ServerTag + RequestID + Recovery + SecureHeaders
	assert.Len(t, gs.Public.Engine.Handlers, 4)
	// Internal/Technical: ServerTag + RequestID + Recovery
	assert.Len(t, gs.Internal.Engine.Handlers, 3)
	assert.Len(t, gs.Technical.Engine.Handlers, 3)
}

func TestNewGinService_MiddlewareCount_SecureHeadersDisabled(t *testing.T) {
	gs := NewGinService(LogModeGin, WithSecureHeaders(false))
	// No SecureHeaders on Public
	assert.Len(t, gs.Public.Engine.Handlers, 4)
}

func TestNewGinService_MiddlewareCount_WithMaxBodySize(t *testing.T) {
	gs := NewGinService(LogModeGin, WithMaxBodySize(4<<20))
	// Public: +1 for MaxBodySize on top of GinMode count
	assert.Len(t, gs.Public.Engine.Handlers, 6)
	// Internal unchanged
	assert.Len(t, gs.Internal.Engine.Handlers, 4)
}

// --- Options ---

func TestWithPublicPort_ChangesPort(t *testing.T) {
	gs := NewGinService(LogModeNoop, WithPublicPort(":9090"))
	assert.Equal(t, ":9090", gs.Public.Port)
	assert.Equal(t, ":8081", gs.Internal.Port)
	assert.Equal(t, ":8082", gs.Technical.Port)
}

func TestWithInternalPort_ChangesPort(t *testing.T) {
	gs := NewGinService(LogModeNoop, WithInternalPort(":9091"))
	assert.Equal(t, ":8080", gs.Public.Port)
	assert.Equal(t, ":9091", gs.Internal.Port)
}

func TestWithTechnicalPort_ChangesPort(t *testing.T) {
	gs := NewGinService(LogModeNoop, WithTechnicalPort(":9092"))
	assert.Equal(t, ":9092", gs.Technical.Port)
}

func TestWithAllPorts_ChangesAllPorts(t *testing.T) {
	gs := NewGinService(LogModeNoop,
		WithPublicPort(":9080"),
		WithInternalPort(":9081"),
		WithTechnicalPort(":9082"),
	)
	assert.Equal(t, ":9080", gs.Public.Port)
	assert.Equal(t, ":9081", gs.Internal.Port)
	assert.Equal(t, ":9082", gs.Technical.Port)
}

// --- SecureHeaders ---

func TestSecureHeaders_AddsAllHeaders(t *testing.T) {
	engine := gin.New()
	engine.Use(SecureHeaders())
	engine.GET("/ping", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := performRequest(engine, "GET", "/ping")

	assert.Equal(t, "nosniff", w.Header().Get("X-Content-Type-Options"))
	assert.Equal(t, "DENY", w.Header().Get("X-Frame-Options"))
	assert.Equal(t, "0", w.Header().Get("X-XSS-Protection"))
	assert.Equal(t, "strict-origin-when-cross-origin", w.Header().Get("Referrer-Policy"))
}

func TestSecureHeaders_PresentOnPublicByDefault(t *testing.T) {
	gs := NewGinService(LogModeNoop, WithPublicRoutes(func(r gin.IRouter) {
		r.GET("/ping", func(c *gin.Context) { c.Status(http.StatusOK) })
	}))

	w := performRequest(gs.Public.Engine, "GET", "/ping")
	assert.Equal(t, "nosniff", w.Header().Get("X-Content-Type-Options"))
	assert.Equal(t, "DENY", w.Header().Get("X-Frame-Options"))
}

func TestSecureHeaders_AbsentWhenDisabled(t *testing.T) {
	gs := NewGinService(LogModeNoop, WithSecureHeaders(false), WithPublicRoutes(func(r gin.IRouter) {
		r.GET("/ping", func(c *gin.Context) { c.Status(http.StatusOK) })
	}))

	w := performRequest(gs.Public.Engine, "GET", "/ping")
	assert.Empty(t, w.Header().Get("X-Content-Type-Options"))
}

func TestSecureHeaders_NotOnInternal(t *testing.T) {
	gs := NewGinService(LogModeNoop, WithInternalRoutes(func(r gin.IRouter) {
		r.GET("/ping", func(c *gin.Context) { c.Status(http.StatusOK) })
	}))

	w := performRequest(gs.Internal.Engine, "GET", "/ping")
	assert.Empty(t, w.Header().Get("X-Content-Type-Options"))
}

func TestSecureHeaders_NotOnTechnical(t *testing.T) {
	gs := NewGinService(LogModeNoop)
	w := performRequest(gs.Technical.Engine, "GET", "/health")
	assert.Empty(t, w.Header().Get("X-Content-Type-Options"))
}

// --- MaxBodySize ---

func TestMaxBodySize_AllowsBodyUnderLimit(t *testing.T) {
	engine := gin.New()
	engine.Use(MaxBodySize(100))

	var readErr error
	engine.POST("/upload", func(c *gin.Context) {
		_, readErr = io.ReadAll(c.Request.Body)
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/upload", strings.NewReader("hello"))
	engine.ServeHTTP(w, req)

	assert.NoError(t, readErr)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestMaxBodySize_RejectsBodyOverLimit(t *testing.T) {
	engine := gin.New()
	engine.Use(MaxBodySize(5))

	var readErr error
	engine.POST("/upload", func(c *gin.Context) {
		_, readErr = io.ReadAll(c.Request.Body)
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/upload", strings.NewReader("more than five bytes"))
	engine.ServeHTTP(w, req)

	assert.Error(t, readErr)
}

func TestWithMaxBodySize_AppliedOnPublicOnly(t *testing.T) {
	gs := NewGinService(LogModeNoop,
		WithMaxBodySize(5),
		WithPublicRoutes(func(r gin.IRouter) {
			r.POST("/upload", func(c *gin.Context) {
				_, err := io.ReadAll(c.Request.Body)
				if err != nil {
					c.Status(http.StatusRequestEntityTooLarge)
					return
				}
				c.Status(http.StatusOK)
			})
		}),
		WithInternalRoutes(func(r gin.IRouter) {
			r.POST("/upload", func(c *gin.Context) {
				io.ReadAll(c.Request.Body)
				c.Status(http.StatusOK)
			})
		}),
	)

	bigBody := strings.NewReader("more than five bytes")

	// Public rejects oversized body
	wPub := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/upload", bigBody)
	gs.Public.Engine.ServeHTTP(wPub, req)
	assert.Equal(t, http.StatusRequestEntityTooLarge, wPub.Code)

	// Internal has no limit — same body size succeeds
	bigBody2 := strings.NewReader("more than five bytes")
	wInt := httptest.NewRecorder()
	req2 := httptest.NewRequest("POST", "/upload", bigBody2)
	gs.Internal.Engine.ServeHTTP(wInt, req2)
	assert.Equal(t, http.StatusOK, wInt.Code)
}

// --- WithPublicRoutes / WithInternalRoutes / WithTechnicalRoutes ---

func TestWithPublicRoutes_RegistersRoutes(t *testing.T) {
	gs := NewGinService(LogModeNoop, WithPublicRoutes(func(r gin.IRouter) {
		r.GET("/hello", func(c *gin.Context) { c.String(http.StatusOK, "world") })
	}))

	w := performRequest(gs.Public.Engine, "GET", "/hello")
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "world", w.Body.String())
}

func TestWithPublicRoutes_NotOnOtherServers(t *testing.T) {
	gs := NewGinService(LogModeNoop, WithPublicRoutes(func(r gin.IRouter) {
		r.GET("/hello", func(c *gin.Context) { c.Status(http.StatusOK) })
	}))

	assert.Equal(t, http.StatusNotFound, performRequest(gs.Internal.Engine, "GET", "/hello").Code)
	assert.Equal(t, http.StatusNotFound, performRequest(gs.Technical.Engine, "GET", "/hello").Code)
}

func TestWithInternalRoutes_RegistersRoutes(t *testing.T) {
	gs := NewGinService(LogModeNoop, WithInternalRoutes(func(r gin.IRouter) {
		r.GET("/stats", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	}))

	assert.Equal(t, http.StatusOK, performRequest(gs.Internal.Engine, "GET", "/stats").Code)
	assert.Equal(t, http.StatusNotFound, performRequest(gs.Public.Engine, "GET", "/stats").Code)
}

func TestWithTechnicalRoutes_RegistersAdditionalRoutes(t *testing.T) {
	gs := NewGinService(LogModeNoop, WithTechnicalRoutes(func(r gin.IRouter) {
		r.GET("/debug/pprof", func(c *gin.Context) { c.Status(http.StatusOK) })
	}))

	// Built-in routes still work
	assert.Equal(t, http.StatusOK, performRequest(gs.Technical.Engine, "GET", "/health").Code)
	// Custom route also works
	assert.Equal(t, http.StatusOK, performRequest(gs.Technical.Engine, "GET", "/debug/pprof").Code)
}

func TestWithPublicRoutes_GetsSecureHeaders(t *testing.T) {
	gs := NewGinService(LogModeNoop, WithPublicRoutes(func(r gin.IRouter) {
		r.GET("/api", func(c *gin.Context) { c.Status(http.StatusOK) })
	}))

	w := performRequest(gs.Public.Engine, "GET", "/api")
	assert.Equal(t, "nosniff", w.Header().Get("X-Content-Type-Options"))
}

// --- Technical routes ---

func TestTechnicalRoutes_HealthAlwaysReturns200(t *testing.T) {
	gs := NewGinService(LogModeNoop)
	w := performRequest(gs.Technical.Engine, "GET", "/health")
	assert.Equal(t, http.StatusOK, w.Code)

	var body map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	assert.Equal(t, "ok", body["status"])
}

func TestTechnicalRoutes_ReadyReturns200ByDefault(t *testing.T) {
	gs := NewGinService(LogModeNoop)
	w := performRequest(gs.Technical.Engine, "GET", "/ready")
	assert.Equal(t, http.StatusOK, w.Code)

	var body map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	assert.Equal(t, "ready", body["status"])
}

func TestTechnicalRoutes_ReadyReturns503WhenCheckFails(t *testing.T) {
	gs := NewGinService(LogModeNoop, WithReadinessCheck(func() bool { return false }))
	w := performRequest(gs.Technical.Engine, "GET", "/ready")
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)

	var body map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	assert.Equal(t, "not_ready", body["status"])
}

func TestTechnicalRoutes_ReadyReturns200WhenCheckPasses(t *testing.T) {
	gs := NewGinService(LogModeNoop, WithReadinessCheck(func() bool { return true }))
	w := performRequest(gs.Technical.Engine, "GET", "/ready")
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestTechnicalRoutes_VersionNotRegisteredWithoutOption(t *testing.T) {
	gs := NewGinService(LogModeNoop)
	w := performRequest(gs.Technical.Engine, "GET", "/version")
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestTechnicalRoutes_VersionReturnsVersionString(t *testing.T) {
	gs := NewGinService(LogModeNoop, WithVersion("v1.2.3"))
	w := performRequest(gs.Technical.Engine, "GET", "/version")
	assert.Equal(t, http.StatusOK, w.Code)

	var body map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	assert.Equal(t, "v1.2.3", body["version"])
}

func TestTechnicalRoutes_NotRegisteredOnPublic(t *testing.T) {
	gs := NewGinService(LogModeNoop)
	assert.Equal(t, http.StatusNotFound, performRequest(gs.Public.Engine, "GET", "/health").Code)
	assert.Equal(t, http.StatusNotFound, performRequest(gs.Public.Engine, "GET", "/ready").Code)
}

// --- RequestID ---

func TestRequestID_GeneratesIDWhenAbsent(t *testing.T) {
	engine := gin.New()
	engine.Use(RequestID())

	var gotID string
	engine.GET("/ping", func(c *gin.Context) {
		gotID = c.GetString("request_id")
		c.Status(http.StatusOK)
	})

	w := performRequest(engine, "GET", "/ping")
	assert.NotEmpty(t, gotID)
	assert.Equal(t, gotID, w.Header().Get("X-Request-ID"))
}

func TestRequestID_PropagatesExistingID(t *testing.T) {
	engine := gin.New()
	engine.Use(RequestID())

	var gotID string
	engine.GET("/ping", func(c *gin.Context) {
		gotID = c.GetString("request_id")
		c.Status(http.StatusOK)
	})

	w := performRequestWithHeaders(engine, "GET", "/ping", map[string]string{"X-Request-ID": "my-trace-id"})
	assert.Equal(t, "my-trace-id", gotID)
	assert.Equal(t, "my-trace-id", w.Header().Get("X-Request-ID"))
}

func TestRequestID_IDsAreUnique(t *testing.T) {
	engine := gin.New()
	engine.Use(RequestID())
	engine.GET("/ping", func(c *gin.Context) { c.Status(http.StatusOK) })

	ids := make(map[string]struct{})
	for range 20 {
		w := performRequest(engine, "GET", "/ping")
		ids[w.Header().Get("X-Request-ID")] = struct{}{}
	}
	assert.Len(t, ids, 20)
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
	engine.Use(RequestID())
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
	assert.Contains(t, entry, "request_id")
	assert.Contains(t, entry, "query")
	assert.Contains(t, entry, "bytes")
	assert.Contains(t, entry, "user_agent")
}

func TestGinSlogMiddleware_LogsQueryString(t *testing.T) {
	logger, buf := newTestLogger()
	engine := gin.New()
	engine.Use(GinSlogMiddleware(logger))
	engine.GET("/search", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/search?q=hello&page=2", nil)
	engine.ServeHTTP(w, req)

	var entry map[string]any
	require.NoError(t, json.NewDecoder(buf).Decode(&entry))
	assert.Equal(t, "q=hello&page=2", entry["query"])
}

func TestGinSlogMiddleware_LogsRequestID(t *testing.T) {
	logger, buf := newTestLogger()
	engine := gin.New()
	engine.Use(RequestID())
	engine.Use(GinSlogMiddleware(logger))
	engine.GET("/ping", func(c *gin.Context) { c.Status(http.StatusOK) })

	performRequestWithHeaders(engine, "GET", "/ping", map[string]string{"X-Request-ID": "trace-abc"})

	var entry map[string]any
	require.NoError(t, json.NewDecoder(buf).Decode(&entry))
	assert.Equal(t, "trace-abc", entry["request_id"])
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
