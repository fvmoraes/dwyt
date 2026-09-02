package status

import (
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"
)

func TestGetHeadroomMetricsUsesSelectedPort(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"persistent_savings":{"lifetime":{"tokens_saved":42}},"requests":{"total":7}}`))
	}))
	server.Listener = listener
	server.Start()
	t.Cleanup(server.Close)

	previous := HeadroomPort()
	port := listener.Addr().(*net.TCPAddr).Port
	SetHeadroomPort(port)
	t.Cleanup(func() { SetHeadroomPort(previous) })

	metrics := GetHeadroomMetrics()
	if !metrics.Running || metrics.Port != port || metrics.TokensSaved != 42 || metrics.RequestsDone != 7 {
		t.Fatalf("GetHeadroomMetrics() = %+v, want running metrics from port %d", metrics, port)
	}
}

func TestGetHeadroomMetricsHasBoundedHTTPAttempt(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	server.Listener = listener
	server.Start()
	t.Cleanup(server.Close)

	if headroomMetricsHTTPClient.Timeout != headroomMetricsTimeout || headroomMetricsTimeout > 2*time.Second {
		t.Fatalf("metrics HTTP timeout = %s, want a short bounded timeout", headroomMetricsHTTPClient.Timeout)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	client := &http.Client{Timeout: 40 * time.Millisecond}
	start := time.Now()
	metrics := getHeadroomMetrics(port, client)
	elapsed := time.Since(start)
	if metrics.Running {
		t.Fatalf("stalled stats endpoint reported running: %+v", metrics)
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("stalled stats request took %s, want bounded attempt", elapsed)
	}
	select {
	case <-started:
		// The request reached the endpoint and returned because of the timeout.
	default:
		t.Fatalf("stats endpoint was not reached on port %s", strconv.Itoa(port))
	}
}

func TestSetHeadroomPortRejectsInvalidValues(t *testing.T) {
	previous := HeadroomPort()
	SetHeadroomPort(8788)
	t.Cleanup(func() { SetHeadroomPort(previous) })
	SetHeadroomPort(0)
	if got := HeadroomPort(); got != 8788 {
		t.Fatalf("HeadroomPort() = %d after invalid set, want 8788", got)
	}
}

func TestCommandOutputWithTimeoutBoundsHungTool(t *testing.T) {
	if os.Getenv("DWYT_STATUS_TIMEOUT_HELPER") == "1" {
		select {}
	}
	t.Setenv("DWYT_STATUS_TIMEOUT_HELPER", "1")

	start := time.Now()
	_, err := commandOutputWithTimeout(40*time.Millisecond, "", os.Args[0], "-test.run=^TestCommandOutputWithTimeoutBoundsHungTool$")
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected timeout error from a hung tool probe")
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("hung tool probe took %s, want bounded execution", elapsed)
	}
}
