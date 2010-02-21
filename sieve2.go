// Copyright 2009 Anh Hai Trinh. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// An efficient Eratosthenesque prime sieve using channels.
// Discussion: <http://blog.onideas.ws/eratosthenes.go>

// This program will print all primes <= n, where n := flag.Arg(0).
// If the flag -n is given, it will print the nth prime only.

package main

import (
	"container/heap"
	"container/vector"
	"flag"
	"fmt"
	"os"
	"runtime"
	"strconv"
)

var nth = flag.Bool("n", false, "print the nth prime only")
var nCPU = flag.Int("ncpu", 1, "number of CPUs to use")

// Wheel to quickly generate numbers coprime to 2, 3, 5 and 7.
// Starting from 13, we successively add wheel[i] to get 17, 19, 23, ...
var wheel = []int{
	4, 2, 4, 6, 2, 6, 4, 2, 4, 6, 6, 2, 6, 4, 2, 6, 4, 6, 8, 4, 2, 4, 2, 4, 8,
	6, 4, 6, 2, 4, 6, 2, 6, 6, 4, 2, 4, 6, 2, 6, 4, 2, 4, 2, 10, 2, 10, 2,
}

// Return a chan int of values (n + k * wheel[i]) for successive i.
func spin(n, k, i, bufsize int) chan int {
	out := make(chan int, bufsize)
	go func() {
		for {
			for ; i < 48; i++ {
				out <- n
				n += k * wheel[i]
			}
			i = 0
		}
	}()
	return out
}

// Return a chan of numbers coprime to 2, 3, 5 and 7, starting from 13.
// coprime2357() -> 13, 17, 19, 23, 25, 31, 35, 37, 41, 47, ...
func coprime2357() chan int { return spin(13, 1, 0, 750) }

// Map (p % 210) to a corresponding wheel position.
// A prime number can only be one of these value (mod 210).
var wheelpos = map[int]int{
	1: 46, 11: 47, 13: 0, 17: 1, 19: 2, 23: 3, 29: 4, 31: 5, 37: 6, 41: 7,
	43: 8, 47: 9, 53: 10, 59: 11, 61: 12, 67: 13, 71: 14, 73: 15, 79: 16,
	83: 17, 89: 18, 97: 19, 101: 20, 103: 21, 107: 22, 109: 23, 113: 24,
	121: 25, 127: 26, 131: 27, 137: 28, 139: 29, 143: 30, 149: 31, 151: 32,
	157: 33, 163: 34, 167: 35, 169: 36, 173: 37, 179: 38, 181: 39, 187: 40,
	191: 41, 193: 42, 197: 43, 199: 44, 209: 45,
}

// Return a chan of multiples of a prime p that are relative prime
// to 2, 3, 5 and 7, starting from (p * p).
// multiples(11) -> 121, 143, 187, 209, 253, 319, 341, 407, 451, 473, ...
// multiples(13) -> 169, 221, 247, 299, 377, 403, 481, 533, 559, 611, ...
func multiples(p int) chan int { return spin(p*p, p, wheelpos[p%210], 50) }

type PeekCh struct {
	head int
	ch   chan int
}

// Heap of PeekCh, sorting by head values.
// We could use a simple Vector, but the heap does lots of comparisons
// and using a IntVector for the head values is a bit faster.
type PeekChHeap struct {
	heads *vector.IntVector
	chs   *vector.Vector
}

func NewPeekChHeap() *PeekChHeap {
	h := new(PeekChHeap)
	h.heads = new(vector.IntVector)
	h.chs = new(vector.Vector)
	return h
}

func (h *PeekChHeap) Push(x interface{}) {
	h.heads.Push(x.(*PeekCh).head)
	h.chs.Push(x.(*PeekCh).ch)
}

func (h *PeekChHeap) Pop() interface{} {
	return &PeekCh{h.heads.Pop(), h.chs.Pop().(chan int)}
}

func (h *PeekChHeap) Len() int { return h.heads.Len() }

func (h *PeekChHeap) Less(i, j int) bool { return h.heads.Less(i, j) }

func (h *PeekChHeap) Swap(i, j int) {
	h.heads.Swap(i, j)
	h.chs.Swap(i, j)
}

// Use a goroutine to receive values from `out` and store them
// in an auto-expanding buffer, so that sending to `out` never blocks.
// Return a channel which serves as a sending proxy to to `out`.
// See this discussion:
// <http://rogpeppe.wordpress.com/2010/02/10/unlimited-buffering-with-low-overhead>
func sendproxy(out chan<- int) chan<- int {
	in := make(chan int, 100)
	go func() {
		var buf = make([]int, 1000)
		var i, n int
		var c chan<- int
		var ok bool
		for {
			select {
			case e := <-in:
				for added := 0; added < 1000; added++ {
					if closed(in) {
						in = nil
						if n == 0 {
							close(out)
							return
						}
						break
					}
					buf[(i+n) % len(buf)] = e
					n++
					if n == len(buf) {
						// buffer full: expand it
						b := make([]int, n*2)
						copy(b, buf[i:])
						copy(b[n-i:], buf[0:i])
						i = 0
						buf = b
					}
					if e, ok = <-in; !ok {
						break
					}
				}
				c = out
			case c <- buf[i]:
				for {
					if i++; i == len(buf) {
						i = 0
					}
					n--
					if n == 0 {
						// buffer empty: don't try to send on output
						if in == nil {
							close(out)
							return
						}
						c = nil
						break
					}
					if ok = c <- buf[i]; !ok {
						break
					}
				}
			}
		}
	}()
	return in
}

// Return a chan int of primes.
func Sieve() chan int {
	// The output values.
	out := make(chan int, 100)
	out <- 2
	out <- 3
	out <- 5
	out <- 7
	out <- 11

	// The channel of all composites to be eliminated in increasing order.
	composites := make(chan int, 500)

	// The feedback loop.
	primes := make(chan int, 100)
	primes <- 11

	// Merge channels of multiples of `primes` into `composites`.
	go func() {
		h := NewPeekChHeap()
		min := 143
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
		primes := sendproxy(primes)
		// In order to generate the nth prime we only need multiples of
		// primes ≤ sqrt(nth prime).  Thus, the merging goroutine will
		// receive from this channel much slower than this goroutine
		// will send to it, making the buffer accumulates and blocks this
		// goroutine from sending to `primes`, causing a deadlock.  The
		// solution is to use a proxy goroutine to do automatic buffering.
		candidates := coprime2357()
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
