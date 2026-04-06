// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package proxy

import (
	"net"
	"net/http"
	"sync"
	"testing"

	"github.com/achetronic/vrata/internal/model"
	io_prometheus_client "github.com/prometheus/client_model/go"
)

func TestSameTimeouts_BothNil(t *testing.T) {
	if !sameTimeouts(nil, nil) {
		t.Error("nil == nil should be true")
	}
}

func TestSameTimeouts_OneNil(t *testing.T) {
	a := &model.ListenerTimeouts{ClientHeader: "10s"}
	if sameTimeouts(a, nil) {
		t.Error("non-nil != nil")
	}
	if sameTimeouts(nil, a) {
		t.Error("nil != non-nil")
	}
}

func TestSameTimeouts_Equal(t *testing.T) {
	a := &model.ListenerTimeouts{ClientHeader: "10s", ClientRequest: "60s", ClientResponse: "60s", IdleBetweenRequests: "120s"}
	b := &model.ListenerTimeouts{ClientHeader: "10s", ClientRequest: "60s", ClientResponse: "60s", IdleBetweenRequests: "120s"}
	if !sameTimeouts(a, b) {
		t.Error("identical timeouts should be equal")
	}
}

func TestSameTimeouts_Different(t *testing.T) {
	a := &model.ListenerTimeouts{ClientHeader: "10s"}
	b := &model.ListenerTimeouts{ClientHeader: "20s"}
	if sameTimeouts(a, b) {
		t.Error("different clientHeader should not be equal")
	}
}

func TestSameListener_TimeoutChange(t *testing.T) {
	a := model.Listener{Port: 80, Timeouts: &model.ListenerTimeouts{ClientHeader: "10s"}}
	b := model.Listener{Port: 80, Timeouts: &model.ListenerTimeouts{ClientHeader: "20s"}}
	if sameListener(a, b) {
		t.Error("listener with different timeouts should trigger restart")
	}
}

func TestSameListener_TimeoutNilToSet(t *testing.T) {
	a := model.Listener{Port: 80}
	b := model.Listener{Port: 80, Timeouts: &model.ListenerTimeouts{ClientHeader: "10s"}}
	if sameListener(a, b) {
		t.Error("adding timeouts should trigger restart")
	}
}

func TestConnTrackingListener_RecordsMetrics(t *testing.T) {
	cfg := &model.ListenerMetrics{Path: "/metrics"}
	mc := NewMetricsCollector(cfg)

	// Create a real TCP listener on a random port.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	ctl := &connTrackingListener{
		Listener:     ln,
		mc:           mc,
		listenerName: "test",
		address:      ln.Addr().String(),
	}

	// Accept in a goroutine.
	accepted := make(chan net.Conn, 1)
	go func() {
		c, err := ctl.Accept()
		if err != nil {
			return
		}
		accepted <- c
	}()

	// Connect.
	client, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	serverConn := <-accepted

	// Verify connection counter incremented.
	m := &io_prometheus_client.Metric{}
	if err := mc.listenerConnections.WithLabelValues("test", ln.Addr().String()).Write(m); err != nil {
		t.Fatal(err)
	}
	if m.GetCounter().GetValue() != 1 {
		t.Errorf("expected 1 connection, got %v", m.GetCounter().GetValue())
	}

	// Verify active gauge incremented.
	m = &io_prometheus_client.Metric{}
	if err := mc.listenerActive.WithLabelValues("test", ln.Addr().String()).Write(m); err != nil {
		t.Fatal(err)
	}
	if m.GetGauge().GetValue() != 1 {
		t.Errorf("expected 1 active, got %v", m.GetGauge().GetValue())
	}

	// Close server-side connection — active gauge should decrement.
	serverConn.Close()

	m = &io_prometheus_client.Metric{}
	if err := mc.listenerActive.WithLabelValues("test", ln.Addr().String()).Write(m); err != nil {
		t.Fatal(err)
	}
	if m.GetGauge().GetValue() != 0 {
		t.Errorf("expected 0 active after close, got %v", m.GetGauge().GetValue())
	}
}

func TestConnTrackingListener_CloseIdempotent(t *testing.T) {
	cfg := &model.ListenerMetrics{Path: "/metrics"}
	mc := NewMetricsCollector(cfg)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	ctl := &connTrackingListener{
		Listener:     ln,
		mc:           mc,
		listenerName: "test",
		address:      ln.Addr().String(),
	}

	accepted := make(chan net.Conn, 1)
	go func() {
		c, _ := ctl.Accept()
		if c != nil {
			accepted <- c
		}
	}()

	client, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	serverConn := <-accepted

	// Close twice — should not double-decrement.
	serverConn.Close()
	serverConn.Close()

	m := &io_prometheus_client.Metric{}
	if err := mc.listenerActive.WithLabelValues("test", ln.Addr().String()).Write(m); err != nil {
		t.Fatal(err)
	}
	if m.GetGauge().GetValue() != 0 {
		t.Errorf("double close should not double-decrement, got %v", m.GetGauge().GetValue())
	}
}

func TestConnStateTLSErrorDetection(t *testing.T) {
	cfg := &model.ListenerMetrics{Path: "/metrics"}
	mc := NewMetricsCollector(cfg)

	listenerName := "tls-listener"
	address := ":9443"
	connSeen := &sync.Map{}

	connState := func(conn net.Conn, state http.ConnState) {
		switch state {
		case http.StateNew:
			connSeen.Store(conn, true)
		case http.StateActive:
			connSeen.Delete(conn)
		case http.StateClosed:
			if _, wasNew := connSeen.LoadAndDelete(conn); wasNew {
				mc.RecordTLSError(listenerName, address)
			}
		}
	}

	fakeConn := &net.TCPConn{}

	// Simulate successful TLS: New → Active → Closed (no TLS error).
	connState(fakeConn, http.StateNew)
	connState(fakeConn, http.StateActive)
	connState(fakeConn, http.StateClosed)

	m := &io_prometheus_client.Metric{}
	if err := mc.listenerTLSErrors.WithLabelValues(listenerName, address).Write(m); err != nil {
		t.Fatal(err)
	}
	if m.GetCounter().GetValue() != 0 {
		t.Errorf("successful TLS should not record error, got %v", m.GetCounter().GetValue())
	}

	// Simulate TLS handshake failure: New → Closed (never reached Active).
	fakeConn2 := &net.TCPConn{}
	connState(fakeConn2, http.StateNew)
	connState(fakeConn2, http.StateClosed)

	m = &io_prometheus_client.Metric{}
	if err := mc.listenerTLSErrors.WithLabelValues(listenerName, address).Write(m); err != nil {
		t.Fatal(err)
	}
	if m.GetCounter().GetValue() != 1 {
		t.Errorf("TLS handshake failure should record 1 error, got %v", m.GetCounter().GetValue())
	}

	// Simulate another TLS failure.
	fakeConn3 := &net.TCPConn{}
	connState(fakeConn3, http.StateNew)
	connState(fakeConn3, http.StateClosed)

	m = &io_prometheus_client.Metric{}
	if err := mc.listenerTLSErrors.WithLabelValues(listenerName, address).Write(m); err != nil {
		t.Fatal(err)
	}
	if m.GetCounter().GetValue() != 2 {
		t.Errorf("expected 2 TLS errors, got %v", m.GetCounter().GetValue())
	}
}
