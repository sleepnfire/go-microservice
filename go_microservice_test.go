package go_microservice

import (
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	mgin "github.com/sleepnfire/go-microservice/gin"
)

// --- NewApiService ---

func TestNewApiService_MapsFields(t *testing.T) {
	ag := mgin.NewGinService(mgin.LogModeGin).Public
	as := NewApiService(ag)

	assert.Equal(t, "Public", as.name)
	assert.Equal(t, ":8080", as.port)
	assert.Equal(t, ":8080", as.server.Addr)
	assert.Equal(t, ag.Engine, as.server.Handler)
}

func TestNewApiService_HasDefaultTimeouts(t *testing.T) {
	ag := mgin.NewGinService(mgin.LogModeGin).Public
	as := NewApiService(ag)

	assert.Equal(t, defaultReadHeaderTimeout, as.server.ReadHeaderTimeout)
	assert.Equal(t, defaultReadTimeout, as.server.ReadTimeout)
	assert.Equal(t, defaultWriteTimeout, as.server.WriteTimeout)
	assert.Equal(t, defaultIdleTimeout, as.server.IdleTimeout)
}

// --- NewMicroService ---

func TestNewMicroService_MapsAllThreeServers(t *testing.T) {
	gs := mgin.NewGinService(mgin.LogModeGin)
	ms := NewMicroService(gs)
	require.NotNil(t, ms)

	assert.Equal(t, "Public", ms.Public.name)
	assert.Equal(t, "Internal", ms.Internal.name)
	assert.Equal(t, "Technical", ms.Technical.name)

	assert.Equal(t, ":8080", ms.Public.port)
	assert.Equal(t, ":8081", ms.Internal.port)
	assert.Equal(t, ":8082", ms.Technical.port)
}

func TestNewMicroService_FromNewGinService_GinMode(t *testing.T) {
	ms := NewMicroService(mgin.NewGinService(mgin.LogModeGin))
	assert.NotNil(t, ms)
}

func TestNewMicroService_FromNewGinService_SlogMode(t *testing.T) {
	ms := NewMicroService(mgin.NewGinService(mgin.LogModeSlog))
	assert.NotNil(t, ms)
}

func TestNewMicroService_WithPortOptions(t *testing.T) {
	gs := mgin.NewGinService(mgin.LogModeNoop,
		mgin.WithPublicPort(":19070"),
		mgin.WithInternalPort(":19071"),
		mgin.WithTechnicalPort(":19072"),
	)
	ms := NewMicroService(gs)
	require.NotNil(t, ms)

	assert.Equal(t, ":19070", ms.Public.port)
	assert.Equal(t, ":19071", ms.Internal.port)
	assert.Equal(t, ":19072", ms.Technical.port)
}

// --- Start / Stop integration ---

// waitForPort polls until the TCP address accepts connections or timeout elapses.
func waitForPort(t *testing.T, addr string, timeout time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			conn.Close()
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

// portClosed polls until the TCP address stops accepting connections or timeout elapses.
func portClosed(addr string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err != nil {
			return true
		}
		conn.Close()
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

func newTestMicroService(publicPort, internalPort, technicalPort string) *MicroService {
	gs := mgin.NewGinService(mgin.LogModeNoop,
		mgin.WithPublicPort(publicPort),
		mgin.WithInternalPort(internalPort),
		mgin.WithTechnicalPort(technicalPort),
	)
	return NewMicroService(gs)
}

func TestStartStop_ServersListenAndRespond(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ms := newTestMicroService(":19080", ":19081", ":19082")
	ms.Start()
	t.Cleanup(func() { ms.Stop() })

	addrs := []string{"127.0.0.1:19080", "127.0.0.1:19081", "127.0.0.1:19082"}
	for _, addr := range addrs {
		assert.True(t, waitForPort(t, addr, 2*time.Second), "server not up on "+addr)
	}

	for _, addr := range addrs {
		resp, err := http.Get("http://" + addr + "/")
		if err == nil {
			resp.Body.Close()
		}
		assert.NoError(t, err)
	}
}

func TestStartStop_StopReturnsNil(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ms := newTestMicroService(":19083", ":19084", ":19085")
	ms.Start()

	for _, addr := range []string{"127.0.0.1:19083", "127.0.0.1:19084", "127.0.0.1:19085"} {
		waitForPort(t, addr, 2*time.Second)
	}

	assert.NoError(t, ms.Stop())
}

func TestStartStop_PortsClosedAfterStop(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ms := newTestMicroService(":19086", ":19087", ":19088")
	ms.Start()

	for _, addr := range []string{"127.0.0.1:19086", "127.0.0.1:19087", "127.0.0.1:19088"} {
		waitForPort(t, addr, 2*time.Second)
	}

	ms.Stop()

	for _, addr := range []string{"127.0.0.1:19086", "127.0.0.1:19087", "127.0.0.1:19088"} {
		assert.True(t, portClosed(addr, 2*time.Second), "port still open: "+addr)
	}
}

func TestStartStop_StopIdempotent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ms := newTestMicroService(":19089", ":19090", ":19091")
	ms.Start()

	for _, addr := range []string{"127.0.0.1:19089", "127.0.0.1:19090", "127.0.0.1:19091"} {
		waitForPort(t, addr, 2*time.Second)
	}

	assert.NotPanics(t, func() {
		ms.Stop()
		ms.Stop()
	})
}

func TestWaitForShutdown_StopsOnSIGINT(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ms := newTestMicroService(":19092", ":19093", ":19094")
	ms.Start()

	for _, addr := range []string{"127.0.0.1:19092", "127.0.0.1:19093", "127.0.0.1:19094"} {
		waitForPort(t, addr, 2*time.Second)
	}

	done := make(chan error, 1)
	go func() { done <- ms.WaitForShutdown() }()

	time.Sleep(50 * time.Millisecond)

	p, err := os.FindProcess(os.Getpid())
	require.NoError(t, err)
	require.NoError(t, p.Signal(syscall.SIGINT))

	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("WaitForShutdown did not return after SIGINT")
	}
}

// --- Startup failure / readiness on stop / bind address ---

func TestStart_PortInUseReportedOnErrors(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ln, err := net.Listen("tcp", "127.0.0.1:19095")
	require.NoError(t, err)
	defer ln.Close()

	ms := newTestMicroService("127.0.0.1:19095", ":19096", ":19097")
	ms.Start()
	t.Cleanup(func() { ms.Stop() })

	select {
	case err := <-ms.Errors():
		assert.Contains(t, err.Error(), "Public")
	case <-time.After(3 * time.Second):
		t.Fatal("expected a startup error on Errors()")
	}
}

func TestWaitForShutdown_ReturnsOnStartupFailure(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ln, err := net.Listen("tcp", "127.0.0.1:19098")
	require.NoError(t, err)
	defer ln.Close()

	ms := newTestMicroService(":19099", "127.0.0.1:19098", ":19100")
	ms.Start()

	done := make(chan error, 1)
	go func() { done <- ms.WaitForShutdown() }()

	select {
	case err := <-done:
		assert.Error(t, err)
		assert.True(t, portClosed("127.0.0.1:19099", 2*time.Second))
	case <-time.After(5 * time.Second):
		t.Fatal("WaitForShutdown did not return after startup failure")
	}
}

func TestStop_MarksNotReady(t *testing.T) {
	gs := mgin.NewGinService(mgin.LogModeNoop)
	ms := NewMicroService(gs)
	require.NoError(t, ms.Stop()) // servers never started: Shutdown is a no-op

	w := httptest.NewRecorder()
	gs.Technical.Engine.ServeHTTP(w, httptest.NewRequest("GET", "/ready", nil))
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestBindAddress_LoopbackOnly(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ms := newTestMicroService("127.0.0.1:19101", "127.0.0.1:19102", "127.0.0.1:19103")
	ms.Start()
	t.Cleanup(func() { ms.Stop() })

	assert.True(t, waitForPort(t, "127.0.0.1:19103", 2*time.Second))
	assert.Equal(t, "127.0.0.1:19103", ms.Technical.server.Addr)
}
