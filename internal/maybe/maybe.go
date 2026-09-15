// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

// Package maybe provides an optional value, modelled on Haskell's Maybe.
//
// A [Maybe] is either "Just" a value or "Nothing", and - crucially - the zero
// value is Nothing, so a field that has never been set reads as absent rather
// than as a plausible-looking zero.  That makes it a replacement for sentinel
// values such as G_UNKNOWN, where "unknown" and "-999999 miles per hour" are
// the same bit pattern and arithmetic on one silently produces the other.
//
// The Functor, Applicative and Monad operations are package-level functions
// rather than methods, because Go methods cannot introduce type parameters of
// their own:
//
//	maybe.Fmap(f, m)        -- f <$> m
//	maybe.Ap(mf, m)         -- mf <*> m
//	maybe.LiftA2(f, ma, mb) -- liftA2 f ma mb
//	maybe.Bind(m, f)        -- m >>= f
//
// Maybe[T] is comparable whenever T is, so two Maybes can be compared with ==.
package maybe

import "fmt"

// Maybe is an optional value of type T: either Just a value, or Nothing.
// The zero value is Nothing.
type Maybe[T any] struct {
	value T
	just  bool
}

// Just returns a Maybe holding value.
func Just[T any](value T) Maybe[T] {
	return Maybe[T]{value: value, just: true}
}

// Nothing returns an empty Maybe.  It is the zero value of Maybe[T], so it is
// only needed where a value has to be written explicitly.
func Nothing[T any]() Maybe[T] {
	var nothing Maybe[T]

	return nothing
}

// Pure is Applicative's pure, and a synonym for [Just].
func Pure[T any](value T) Maybe[T] {
	return Just(value)
}

// IsJust reports whether m holds a value.
func (m Maybe[T]) IsJust() bool {
	return m.just
}

// IsNothing reports whether m is empty.
func (m Maybe[T]) IsNothing() bool {
	return !m.just
}

// Get returns the value and whether it was present, in the style of a Go
// comma-ok lookup.  The value is the zero value of T when m is Nothing.
func (m Maybe[T]) Get() (T, bool) {
	return m.value, m.just
}

// Or returns m if it holds a value, and alternative otherwise; it is
// Alternative's (<|>).
func (m Maybe[T]) Or(alternative Maybe[T]) Maybe[T] {
	if m.just {
		return m
	}

	return alternative
}

// String renders m as "Just <value>" or "Nothing", after Haskell's Show.
func (m Maybe[T]) String() string {
	if !m.just {
		return "Nothing"
	}

	return fmt.Sprintf("Just %v", m.value)
}

// FromMaybe returns the value held by m, or defaultValue if m is Nothing.
func FromMaybe[T any](defaultValue T, m Maybe[T]) T {
	if m.just {
		return m.value
	}

	return defaultValue
}

// FromJust returns the value held by m, and panics if m is Nothing.
// Prefer [Maybe.Get] or [FromMaybe] unless the value is known to be present.
func FromJust[T any](m Maybe[T]) T {
	if !m.just {
		panic("maybe.FromJust: Nothing")
	}

	return m.value
}

// Fold applies f to the value held by m, or returns defaultValue if m is
// Nothing.  It is Haskell's maybe function, which cannot be named Maybe here
// because that is the type.
func Fold[A, B any](defaultValue B, f func(A) B, m Maybe[A]) B {
	if !m.just {
		return defaultValue
	}

	return f(m.value)
}

// Fmap applies f to the value held by m, if any; it is Functor's (<$>).
func Fmap[A, B any](f func(A) B, m Maybe[A]) Maybe[B] {
	if !m.just {
		return Nothing[B]()
	}

	return Just(f(m.value))
}

// Ap applies the function held by mf to the value held by ma, and is Nothing
// if either is Nothing; it is Applicative's (<*>).
func Ap[A, B any](mf Maybe[func(A) B], ma Maybe[A]) Maybe[B] {
	if !mf.just || !ma.just {
		return Nothing[B]()
	}

	return Just(mf.value(ma.value))
}

// LiftA2 applies the two-argument f to ma and mb, and is Nothing if either is
// Nothing.
func LiftA2[A, B, C any](f func(A, B) C, ma Maybe[A], mb Maybe[B]) Maybe[C] {
	if !ma.just || !mb.just {
		return Nothing[C]()
	}

	return Just(f(ma.value, mb.value))
}

// Bind passes the value held by m to f, and is Nothing if m is Nothing; it is
// Monad's (>>=).
func Bind[A, B any](m Maybe[A], f func(A) Maybe[B]) Maybe[B] {
	if !m.just {
		return Nothing[B]()
	}

	return f(m.value)
}

// CatMaybes returns the values held by the Just elements of ms.
func CatMaybes[T any](ms []Maybe[T]) []T {
	var values = make([]T, 0, len(ms))

	for _, m := range ms {
		if m.just {
			values = append(values, m.value)
		}
	}

	return values
}

// MapMaybe applies f to every element of values, keeping the Just results.
func MapMaybe[A, B any](f func(A) Maybe[B], values []A) []B {
	var results = make([]B, 0, len(values))

	for _, value := range values {
		if m := f(value); m.just {
			results = append(results, m.value)
		}
	}

	return results
}

// FromPointer returns Just the value pointed to by pointer, or Nothing if
// pointer is nil.  It is for the boundary with code that says "absent" with a
// nil pointer, such as a JSON decoder.
func FromPointer[T any](pointer *T) Maybe[T] {
	if pointer == nil {
		return Nothing[T]()
	}

	return Just(*pointer)
}

// ListToMaybe returns Just the first element of values, or Nothing if values
// is empty.
func ListToMaybe[T any](values []T) Maybe[T] {
	if len(values) == 0 {
		return Nothing[T]()
	}

	return Just(values[0])
}

// MaybeToList returns a one-element slice holding the value of m, or an empty
// slice if m is Nothing.
func MaybeToList[T any](m Maybe[T]) []T {
	if !m.just {
		return nil
	}

	return []T{m.value}
}
