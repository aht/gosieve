// Copyright 2009 Anh Hai Trinh. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// This sieve is Eratosthenesque and only considers odd candidates.

// Print all primes <= n, where n := flag.Arg(0).
// If the flag -n is given, it will print the nth prime only.

package main

import (
	"container/heap"
	"container/ring"
	"container/vector"
	"flag"
	"fmt"
	"os"
	"runtime"
	"strconv"
)

var nth = flag.Bool("n", false, "print the nth prime only")
var nCPU = flag.Int("ncpu", 1, "number of CPUs to use")

// Return a chan of odd numbers, starting from 5.
func odds() chan int {
	out := make(chan int, 500)
	go func() {
		n := 5
		for {
			out <- n
			n += 2
		}
	}()
	return out
}

// Return a chan of odd multiples of the prime number p, starting from p*p.
func multiples(p int) chan int {
	out := make(chan int, 50)
	go func() {
		n := p * p
		for {
			out <- n
			n += 2 * p
		}
	}()
	return out
}

type PeekCh struct {
	head int
	ch   chan int
}

// Heap of PeekCh, sorting by head values.
type PeekChHeap struct {
	*vector.Vector
}

func (h *PeekChHeap) Less(i, j int) bool {
	return h.At(i).(*PeekCh).head < h.At(j).(*PeekCh).head
}

// Return a channel which serves as a sending proxy to `out`.
// Use a goroutine to receive values from `out` and store them
// in an expanding buffer, so that sending to `out` never blocks.
// See this discussion:
// <http://rogpeppe.wordpress.com/2010/02/10/unlimited-buffering-with-low-overhead>
func sendproxy(out chan<- int) chan<- int {
	proxy := make(chan int, 100)
	go func() {
		n := 128 // the allocated size of the circular queue
		first := ring.New(n)
		last := first
		var c chan<- int
		var e int
		for {
			c = out
			if first == last {
				// buffer empty: disable output
				c = nil
			} else {
				e = first.Value.(int)
			}
			select {
			case e = <-proxy:
				last.Value = e
				if last.Next() == first {
					// buffer full: expand it
					last.Link(ring.New(n))
					n *= 2
				}
				last = last.Next()
			case c <- e:
				first = first.Next()
			}
		}
	}()
	return proxy
}

// Return a chan int of primes.
func Sieve() chan int {
	// The output values.
	out := make(chan int, 100)
	out <- 2
	out <- 3

	// The channel of all composites to be eliminated in increasing order.
	composites := make(chan int, 500)

	// The feedback loop.
	primes := make(chan int, 100)
	primes <- 3

	// Merge channels of multiples of `primes` into `composites`.
	go func() {
		h := &PeekChHeap{new(vector.Vector)}
		min := 15
		for {
			m := multiples(<-primes)
			head := <-m
			for min < head {
				composites <- min
				minchan := heap.Pop(h).(*PeekCh)
				min = minchan.head
				minchan.head = <-minchan.ch
				heap.Push(h, minchan)
			}
			for min == head {
				minchan := heap.Pop(h).(*PeekCh)
				min = minchan.head
				minchan.head = <-minchan.ch
				heap.Push(h, minchan)
			}
			composites <- head
			heap.Push(h, &PeekCh{<-m, m})
		}
	}()

	// Sieve out `composites` from `candidates`.
	go func() {
		// In order to generate the nth prime we only need multiples of
		// primes ≤ sqrt(nth prime).  Thus, the merging goroutine will
		// receive from this channel much slower than this goroutine
		// will send to it, making the buffer accumulates and blocks this
		// goroutine from sending to `primes`, causing a deadlock.  The
		// solution is to use a proxy goroutine to do automatic buffering.
		primes := sendproxy(primes)

		candidates := odds()
		p := <-candidates

		for {
			c := <-composites
			for p < c {
				primes <- p
				out <- p
				p = <-candidates
			}
			if p == c {
				p = <-candidates
			}
		}
	}()

	return out
}

func main() {
	flag.Parse()
	n, err := strconv.Atoi(flag.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, "bad argument")
		os.Exit(1)
	}
	runtime.GOMAXPROCS(*nCPU)
	primes := Sieve()
	if *nth {
		for i := 1; i < n; i++ {
			<-primes
		}
		fmt.Println(<-primes)
	} else {
		for {
			p := <-primes
			if p <= n {
				fmt.Println(p)
			} else {
				return
			}
		}
	}
}
