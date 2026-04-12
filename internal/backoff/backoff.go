// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

// Package backoff provides exponential back-off with jitter for reconnection loops.
package backoff

import (
	"math"
	"math/rand"
	"time"
)

const (
	// DefaultBase is the initial delay before the first retry.
	DefaultBase = 1 * time.Second
	// DefaultMax caps the computed delay.
	DefaultMax = 60 * time.Second
	// DefaultJitter is the maximum ±fraction applied to the delay (0.20 = ±20 %).
	DefaultJitter = 0.20
)

// Compute returns the delay for the given attempt number (1-based) using
// exponential back-off with ±20 % jitter, capped at DefaultMax.
//
//	attempt 1 → ~1 s
//	attempt 2 → ~2 s
//	attempt 3 → ~4 s
//	…
//	attempt 7+ → ~60 s (capped)
func Compute(attempt int) time.Duration {
	return ComputeWith(attempt, DefaultBase, DefaultMax, DefaultJitter)
}

// ComputeWith is the parameterised form used internally and in tests.
// jitterFraction must be in [0, 1); a value of 0.20 gives ±20 % jitter.
func ComputeWith(attempt int, base, max time.Duration, jitterFraction float64) time.Duration {
	if attempt < 1 {
		attempt = 1
	}

	// Clamp exponent to avoid overflow of float64.
	exp := float64(attempt - 1)
	if exp > 62 {
		exp = 62
	}

	delay := math.Min(base.Seconds()*math.Pow(2, exp), max.Seconds())

	// ±20 % jitter: random value in [-jitter, +jitter].
	sign := 1.0
	if rand.Intn(2) == 0 {
		sign = -1.0
	}
	jitter := sign * jitterFraction * rand.Float64() * delay

	total := delay + jitter
	if total < 0 {
		total = 0
	}

	return time.Duration(total * float64(time.Second))
}
