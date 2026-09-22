package direwolf

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestCDataNew_Empty(t *testing.T) {
	var cdata = cdata_new(1, nil)

	assert.Empty(t, cdata.data)
}

func TestCDataNew(t *testing.T) {
	// Because sometimes I didn't manage to get the copy right(!)
	var testData = []byte("badger")
	var cdata = cdata_new(1, testData)

	assert.Equal(t, cdata.data, testData)
}

// A sender must never block handing an item over.  By the time it looks,
// the receive thread may have stopped waiting - woken by an earlier item,
// or because its timeout fired - and there is then nobody to take a
// wake-up off an unbuffered channel.  Refs #348.
func TestDLQAppendDoesNotBlockWhenNobodyIsWaiting(t *testing.T) {
	// Whether a sender finds itself alone is a matter of timing, so try
	// the same thing a few times over.
	for range 20 {
		dlq_init()

		// Get the receive thread as far as actually waiting.
		var waited = make(chan bool, 1)

		go func() {
			waited <- dlq_wait_while_empty(t.Context(), time.Now().Add(time.Minute))
		}()

		time.Sleep(20 * time.Millisecond)

		// Nothing has been queued yet, so a waiter that has not returned
		// by now is in the wait rather than having found an item already
		// there - which is what makes the senders below prove anything.
		select {
		case <-waited:
			t.Fatal("dlq_wait_while_empty returned with an empty queue")
		default:
		}

		// Several senders now arrive at once.  Only one of them can hand
		// the waiter its wake-up; the rest must not be left holding one.
		const senders = 10

		var wg sync.WaitGroup

		wg.Add(senders)

		var start = make(chan struct{})

		for range senders {
			go func() {
				defer wg.Done()

				<-start

				append_to_queue(new(dlq_item_t))
			}()
		}

		close(start)

		var appended = make(chan struct{})

		go func() {
			wg.Wait()
			close(appended)
		}()

		select {
		case <-appended:
		case <-time.After(10 * time.Second):
			t.Fatal("append_to_queue blocked with nobody waiting")
		}

		select {
		case timed_out := <-waited:
			assert.False(t, timed_out, "Expected the waiter to be woken, not to time out")
		case <-time.After(10 * time.Second):
			t.Fatal("The waiter was never woken")
		}

		var count int
		for item := dlq_remove(); item != nil; item = dlq_remove() {
			count++
		}

		assert.Equal(t, senders, count)
	}
}

// A wake-up belonging to an item that has since been removed must not cut
// the next wait short, or a timer expiry gets skipped.
func TestDLQStaleWakeUpDoesNotCutShortTheNextWait(t *testing.T) {
	dlq_init()

	append_to_queue(new(dlq_item_t))

	assert.NotNil(t, dlq_remove())

	var wait = 100 * time.Millisecond
	var start = time.Now()

	assert.True(t, dlq_wait_while_empty(t.Context(), time.Now().Add(wait)), "Expected a timeout with an empty queue")
	assert.GreaterOrEqual(t, time.Since(start), wait)
}

// A wait with nothing on the queue is where the receive thread spends most of
// its life, so a cancellation has to reach it there rather than waiting for an
// item that is never coming.
func TestDLQWaitReturnsWhenCancelled(t *testing.T) {
	dlq_init()

	var ctx, cancel = context.WithCancel(t.Context())

	var returned = make(chan bool, 1)

	go func() {
		returned <- dlq_wait_while_empty(ctx, time.Time{}) // No timeout at all.
	}()

	// Nothing has been queued, so a waiter that returns now did so because of
	// the cancellation rather than because it found something.
	cancel()

	select {
	case timed_out := <-returned:
		assert.False(t, timed_out, "Cancellation is not a timeout")
	case <-time.After(time.Second):
		t.Fatal("dlq_wait_while_empty did not return after its context was cancelled")
	}
}

// Items are queued from every receive thread, the AGW server's client
// goroutines, the beacon and the IGate, and deleted by the receive processing
// thread, so the leak accounting has to stand up to all of them at once - as
// does the count of connected-mode data blocks, which the AGW server makes and
// the link state machine frees.
func TestDLQLeakCountersAreSafeForConcurrentUse(t *testing.T) {
	dlq_init()
	t.Cleanup(dlq_init)

	const workers = 4
	const perWorker = 50

	var itemsMade, itemsDeleted = s_new_count.Load(), s_delete_count.Load()
	var cdataMade, cdataDeleted = s_cdata_new_count.Load(), s_cdata_delete_count.Load()

	var wg sync.WaitGroup

	for range workers {
		wg.Go(func() {
			for range perWorker {
				dlq_seize_confirm(0)
			}
		})

		wg.Go(func() {
			for range perWorker {
				cdata_delete(cdata_new(0xF0, []byte("Testing")))
			}
		})
	}

	wg.Go(func() {
		for n := 0; n < workers*perWorker; {
			var item = dlq_remove()
			if item == nil {
				continue
			}

			dlq_delete(item)
			n++
		}
	})

	wg.Wait()

	// Every increment has to land: one lost to a race is a leak reported
	// that never happened, or a real one hidden.
	assert.Equal(t, int64(workers*perWorker), s_new_count.Load()-itemsMade)
	assert.Equal(t, int64(workers*perWorker), s_delete_count.Load()-itemsDeleted)
	assert.Equal(t, int64(workers*perWorker), s_cdata_new_count.Load()-cdataMade)
	assert.Equal(t, int64(workers*perWorker), s_cdata_delete_count.Load()-cdataDeleted)
}
