// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package proxy

import (
	"testing"

	"github.com/achetronic/vrata/internal/model"
)

func TestSameClientAuth_BothNil(t *testing.T) {
	if !sameClientAuth(nil, nil) {
		t.Error("both nil should be same")
	}
}

func TestSameClientAuth_OneNil(t *testing.T) {
	a := &model.ListenerClientAuth{Mode: "require", CA: "/ca.pem"}
	if sameClientAuth(a, nil) {
		t.Error("a non-nil vs nil should differ")
	}
	if sameClientAuth(nil, a) {
		t.Error("nil vs non-nil should differ")
	}
}

func TestSameClientAuth_Equal(t *testing.T) {
	a := &model.ListenerClientAuth{Mode: "require", CA: "/ca.pem"}
	b := &model.ListenerClientAuth{Mode: "require", CA: "/ca.pem"}
	if !sameClientAuth(a, b) {
		t.Error("identical configs should be same")
	}
}

func TestSameClientAuth_DifferentMode(t *testing.T) {
	a := &model.ListenerClientAuth{Mode: "require", CA: "/ca.pem"}
	b := &model.ListenerClientAuth{Mode: "optional", CA: "/ca.pem"}
	if sameClientAuth(a, b) {
		t.Error("different modes should differ")
	}
}

func TestSameClientAuth_DifferentCA(t *testing.T) {
	a := &model.ListenerClientAuth{Mode: "require", CA: "/ca1.pem"}
	b := &model.ListenerClientAuth{Mode: "require", CA: "/ca2.pem"}
	if sameClientAuth(a, b) {
		t.Error("different CA files should differ")
	}
}

func TestSameTLS_WithClientAuth(t *testing.T) {
	base := model.ListenerTLS{Cert: "/cert.pem", Key: "/key.pem"}

	a := base
	a.ClientAuth = &model.ListenerClientAuth{Mode: "require", CA: "/ca.pem"}

	b := base
	b.ClientAuth = &model.ListenerClientAuth{Mode: "require", CA: "/ca.pem"}

	if !sameTLS(&a, &b) {
		t.Error("same TLS + same clientAuth should be same")
	}

	c := base
	c.ClientAuth = &model.ListenerClientAuth{Mode: "optional", CA: "/ca.pem"}

	if sameTLS(&a, &c) {
		t.Error("same TLS + different clientAuth should differ")
	}

	d := base
	if sameTLS(&a, &d) {
		t.Error("clientAuth vs no clientAuth should differ")
	}
}
