// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package proxy

import (
	"testing"

	"github.com/achetronic/vrata/internal/model"
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
