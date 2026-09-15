// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package maybe

import (
	"fmt"
	"testing"
)

func TestZeroValueIsNothing(t *testing.T) {
	var m Maybe[float64]

	if m.IsJust() {
		t.Error("the zero value should be Nothing")
	}

	if m != Nothing[float64]() {
		t.Error("the zero value should equal Nothing()")
	}

	var value, ok = m.Get()
	if ok {
		t.Error("Get on Nothing should report absence")
	}

	if value != 0 {
		t.Errorf("Get on Nothing should give the zero value, got %v", value)
	}
}

func TestJust(t *testing.T) {
	var m = Just(42)

	if !m.IsJust() || m.IsNothing() {
		t.Error("Just should be Just")
	}

	var value, ok = m.Get()
	if !ok || value != 42 {
		t.Errorf("Get gave (%v, %v), want (42, true)", value, ok)
	}
}

// Just(0) is a value that happens to be zero, not an absent one - the whole
// point of the type.
func TestJustZeroIsNotNothing(t *testing.T) {
	if Just(0.0).IsNothing() {
		t.Error("Just(0.0) should not be Nothing")
	}

	if Just(0.0) == Nothing[float64]() {
		t.Error("Just(0.0) should not equal Nothing")
	}
}

func TestComparable(t *testing.T) {
	var one, anotherOne, two = Just(1), Just(1), Just(2)

	if one != anotherOne {
		t.Error("equal values should compare equal")
	}

	if one == two {
		t.Error("different values should not compare equal")
	}
}

func TestFromMaybe(t *testing.T) {
	if got := FromMaybe(7, Just(42)); got != 42 {
		t.Errorf("FromMaybe on Just gave %v, want 42", got)
	}

	if got := FromMaybe(7, Nothing[int]()); got != 7 {
		t.Errorf("FromMaybe on Nothing gave %v, want 7", got)
	}
}

func TestFromJust(t *testing.T) {
	if got := FromJust(Just(42)); got != 42 {
		t.Errorf("FromJust gave %v, want 42", got)
	}
}

func TestFromJustPanicsOnNothing(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("FromJust on Nothing should panic")
		}
	}()

	FromJust(Nothing[int]())
}

func TestOr(t *testing.T) {
	var cases = []struct {
		name string
		m    Maybe[int]
		alt  Maybe[int]
		want Maybe[int]
	}{
		{"just or just", Just(1), Just(2), Just(1)},
		{"just or nothing", Just(1), Nothing[int](), Just(1)},
		{"nothing or just", Nothing[int](), Just(2), Just(2)},
		{"nothing or nothing", Nothing[int](), Nothing[int](), Nothing[int]()},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.m.Or(c.alt); got != c.want {
				t.Errorf("Or gave %v, want %v", got, c.want)
			}
		})
	}
}

func TestFold(t *testing.T) {
	var double = func(n int) int { return n * 2 }

	if got := Fold(-1, double, Just(21)); got != 42 {
		t.Errorf("Fold on Just gave %v, want 42", got)
	}

	if got := Fold(-1, double, Nothing[int]()); got != -1 {
		t.Errorf("Fold on Nothing gave %v, want -1", got)
	}
}

func TestFmap(t *testing.T) {
	var itoa = func(n int) string { return fmt.Sprintf("<%d>", n) }

	if got := Fmap(itoa, Just(42)); got != Just("<42>") {
		t.Errorf("Fmap on Just gave %v, want Just <42>", got)
	}

	if got := Fmap(itoa, Nothing[int]()); got != Nothing[string]() {
		t.Errorf("Fmap on Nothing gave %v, want Nothing", got)
	}
}

// fmap id == id, and fmap (f . g) == fmap f . fmap g
func TestFunctorLaws(t *testing.T) {
	var identity = func(n int) int { return n }
	var f = func(n int) int { return n + 1 }
	var g = func(n int) int { return n * 3 }

	for _, m := range []Maybe[int]{Just(5), Nothing[int]()} {
		if got := Fmap(identity, m); got != m {
			t.Errorf("identity law broken for %v: got %v", m, got)
		}

		var composed = Fmap(func(n int) int { return f(g(n)) }, m)
		if got := Fmap(f, Fmap(g, m)); got != composed {
			t.Errorf("composition law broken for %v: got %v, want %v", m, got, composed)
		}
	}
}

func TestAp(t *testing.T) {
	var add = func(n int) int { return n + 1 }

	if got := Ap(Just(add), Just(41)); got != Just(42) {
		t.Errorf("Ap of two Justs gave %v, want Just 42", got)
	}

	if got := Ap(Just(add), Nothing[int]()); got != Nothing[int]() {
		t.Errorf("Ap with a Nothing argument gave %v, want Nothing", got)
	}

	if got := Ap(Nothing[func(int) int](), Just(41)); got != Nothing[int]() {
		t.Errorf("Ap with a Nothing function gave %v, want Nothing", got)
	}
}

// pure id <*> v == v, and pure f <*> pure x == pure (f x)
func TestApplicativeLaws(t *testing.T) {
	var identity = func(n int) int { return n }

	for _, m := range []Maybe[int]{Just(5), Nothing[int]()} {
		if got := Ap(Pure(identity), m); got != m {
			t.Errorf("identity law broken for %v: got %v", m, got)
		}
	}

	var f = func(n int) int { return n * 2 }
	if got := Ap(Pure(f), Pure(21)); got != Pure(f(21)) {
		t.Errorf("homomorphism law broken: got %v", got)
	}
}

func TestLiftA2(t *testing.T) {
	var sum = func(a, b int) int { return a + b }

	if got := LiftA2(sum, Just(1), Just(2)); got != Just(3) {
		t.Errorf("LiftA2 of two Justs gave %v, want Just 3", got)
	}

	if got := LiftA2(sum, Just(1), Nothing[int]()); got != Nothing[int]() {
		t.Errorf("LiftA2 with a Nothing gave %v, want Nothing", got)
	}

	if got := LiftA2(sum, Nothing[int](), Just(2)); got != Nothing[int]() {
		t.Errorf("LiftA2 with a Nothing gave %v, want Nothing", got)
	}
}

func TestBind(t *testing.T) {
	// Halve only even numbers, so the function itself can fail.
	var halve = func(n int) Maybe[int] {
		if n%2 != 0 {
			return Nothing[int]()
		}

		return Just(n / 2)
	}

	if got := Bind(Just(42), halve); got != Just(21) {
		t.Errorf("Bind gave %v, want Just 21", got)
	}

	if got := Bind(Just(43), halve); got != Nothing[int]() {
		t.Errorf("Bind of a failing function gave %v, want Nothing", got)
	}

	if got := Bind(Nothing[int](), halve); got != Nothing[int]() {
		t.Errorf("Bind on Nothing gave %v, want Nothing", got)
	}
}

// return a >>= f == f a, m >>= return == m, and associativity
func TestMonadLaws(t *testing.T) {
	var f = func(n int) Maybe[int] { return Just(n + 1) }
	var g = func(n int) Maybe[int] { return Just(n * 3) }

	if got := Bind(Pure(5), f); got != f(5) {
		t.Errorf("left identity broken: got %v, want %v", got, f(5))
	}

	for _, m := range []Maybe[int]{Just(5), Nothing[int]()} {
		if got := Bind(m, Pure[int]); got != m {
			t.Errorf("right identity broken for %v: got %v", m, got)
		}

		var right = Bind(m, func(n int) Maybe[int] { return Bind(f(n), g) })
		if got := Bind(Bind(m, f), g); got != right {
			t.Errorf("associativity broken for %v: got %v, want %v", m, got, right)
		}
	}
}

func TestCatMaybes(t *testing.T) {
	var got = CatMaybes([]Maybe[int]{Just(1), Nothing[int](), Just(3)})

	if len(got) != 2 || got[0] != 1 || got[1] != 3 {
		t.Errorf("CatMaybes gave %v, want [1 3]", got)
	}

	if len(CatMaybes([]Maybe[int]{})) != 0 {
		t.Error("CatMaybes of nothing at all should be empty")
	}
}

func TestMapMaybe(t *testing.T) {
	var positive = func(n int) Maybe[int] {
		if n <= 0 {
			return Nothing[int]()
		}

		return Just(n)
	}

	var got = MapMaybe(positive, []int{-1, 2, -3, 4})

	if len(got) != 2 || got[0] != 2 || got[1] != 4 {
		t.Errorf("MapMaybe gave %v, want [2 4]", got)
	}
}

func TestFromPointer(t *testing.T) {
	var value = 1

	if got := FromPointer(&value); got != Just(1) {
		t.Errorf("FromPointer gave %v, want Just 1", got)
	}

	var zero = 0

	if got := FromPointer(&zero); got != Just(0) {
		t.Errorf("FromPointer of a pointer to zero gave %v, want Just 0", got)
	}

	if got := FromPointer[int](nil); got != Nothing[int]() {
		t.Errorf("FromPointer of nil gave %v, want Nothing", got)
	}
}

func TestListToMaybe(t *testing.T) {
	if got := ListToMaybe([]int{1, 2}); got != Just(1) {
		t.Errorf("ListToMaybe gave %v, want Just 1", got)
	}

	if got := ListToMaybe([]int{}); got != Nothing[int]() {
		t.Errorf("ListToMaybe of an empty slice gave %v, want Nothing", got)
	}
}

func TestMaybeToList(t *testing.T) {
	var got = MaybeToList(Just(1))

	if len(got) != 1 || got[0] != 1 {
		t.Errorf("MaybeToList gave %v, want [1]", got)
	}

	if len(MaybeToList(Nothing[int]())) != 0 {
		t.Error("MaybeToList of Nothing should be empty")
	}
}

func TestString(t *testing.T) {
	if got := fmt.Sprintf("%v", Just(42)); got != "Just 42" {
		t.Errorf("formatting Just gave %q, want \"Just 42\"", got)
	}

	if got := fmt.Sprintf("%v", Nothing[int]()); got != "Nothing" {
		t.Errorf("formatting Nothing gave %q, want \"Nothing\"", got)
	}
}
