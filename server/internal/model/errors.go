// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package model

import "errors"

// ErrNotFound is returned when a requested resource does not exist.
var ErrNotFound = errors.New("resource not found")

// ErrInvalidWeight is returned when the sum of destination weights in a route is not 100
// (only enforced when more than one destination is defined).
var ErrInvalidWeight = errors.New("destination weights must sum to 100 when multiple destinations are defined")

// ErrConflictingAction is returned when a route defines more than one action
// mode (destinations, redirect, or directResponse are mutually exclusive).
var ErrConflictingAction = errors.New("a route must define exactly one of destinations, redirect, or directResponse")

// ErrNoActiveSnapshot is returned when the SSE stream is requested but no
// snapshot has been activated yet.
var ErrNoActiveSnapshot = errors.New("no active snapshot configured")
