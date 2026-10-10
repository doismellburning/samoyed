// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package nodeevents

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func event(kind Kind) Event {
	return Event{
		Time: time.Time{}, Kind: kind, Node: "Q1TEST", Port: nil, Local: "", Remote: "", User: "",
		Incoming: false, Role: "", Error: "", Room: "", Number: 0, Type: "", From: "", To: "", At: "",
		Destinations: 0, Neighbours: 0,
	}
}

func TestBusDelivers(t *testing.T) {
	var b = NewBus(nil)

	var a, stopA = b.Subscribe(4)
	var c, stopC = b.Subscribe(4)

	b.Publish(event(LinkUp))

	assert.Equal(t, LinkUp, (<-a).Kind)
	assert.Equal(t, LinkUp, (<-c).Kind)

	stopA()
	stopA() // Twice is harmless.

	b.Publish(event(LinkDown))

	var _, open = <-a
	assert.False(t, open, "an ended subscription is closed")
	assert.Equal(t, LinkDown, (<-c).Kind)

	stopC()
}

func TestBusDropsForSlowSubscriber(t *testing.T) {
	var drops = 0

	var b = NewBus(func() { drops++ })

	var ch, stop = b.Subscribe(1)
	defer stop()

	b.Publish(event(LinkUp))
	b.Publish(event(LinkDown)) // No room: dropped, not waited for.

	assert.Equal(t, 1, drops)
	assert.Equal(t, LinkUp, (<-ch).Kind)
}

func TestNilBus(t *testing.T) {
	var b *Bus
	b.Publish(event(LinkUp))
}

func TestBusConcurrent(t *testing.T) {
	var b = NewBus(nil)

	var ch, stop = b.Subscribe(1000)

	var wg sync.WaitGroup

	for range 10 {
		wg.Go(func() {
			for range 50 {
				b.Publish(event(Heard))
			}
		})
	}

	wg.Wait()
	stop()

	var n = 0
	for range ch {
		n++
	}

	assert.Equal(t, 500, n)
}
