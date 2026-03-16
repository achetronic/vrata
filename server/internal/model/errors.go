package model

import "errors"

// ErrNotFound is returned when a requested resource does not exist.
var ErrNotFound = errors.New("resource not found")

// ErrDuplicateRoute is returned when a route's MatchRule conflicts with an
// existing route in the same group.
var ErrDuplicateRoute = errors.New("a route with the same match rule already exists in this group")

// ErrDuplicateGroup is returned when a group with the same name already exists.
var ErrDuplicateGroup = errors.New("a group with the same name already exists")

// ErrInvalidWeight is returned when the sum of backend weights in a route is not 100
// (only enforced when more than one backend is defined).
var ErrInvalidWeight = errors.New("backend weights must sum to 100 when multiple backends are defined")

// ErrConflictingAction is returned when a route defines more than one action
// mode (backends, redirect, or directResponse are mutually exclusive).
var ErrConflictingAction = errors.New("a route must define exactly one of backends, redirect, or directResponse")
