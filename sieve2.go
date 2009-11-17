// Copyright 2009 Anh Hai Trinh. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Implements the faithful sieve of Eratosthenes.

// References:
// 	<http://www.cs.hmc.edu/~oneill/papers/Sieve-JFP.pdf>
// 	<http://en.literateprograms.org/Sieve_of_Eratosthenes_(Haskell)>

// This program will print all primes <= n, where n := flag.Arg(0).
// If the flag -n is given, it will print the nth prime only.

package main

import (
	"container/heap";
	"container/vector";
	"flag";
	"fmt";
	"math";
	"os";
	"runtime";
	"strconv";
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
	out := make(chan int, bufsize);
	go func() {
		for {
			for ; i < 48; i++ {
				out <- n;
				n += k * wheel[i];
			}
			i = 0;
		}
	}();
	return out;
}

// Return a chan of numbers coprime to 2, 3, 5 and 7, starting from 13.
// coprime2357() -> 13, 17, 23, 25, 31, 35, 37, 41, 47, 53, ...
func coprime2357() chan int	{ return spin(13, 1, 0, 100) }

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
func multiples(p int) chan int	{ return spin(p*p, p, wheelpos[p%210], 20) }

// Given a channel of known primes, merge all channels of their multiples into
// a single channel -- the channel of all composites to be eliminated.
func mergeMultiples(primes chan int) chan int {
	out := make(chan int, 500);
	go func() {
		// Since each channel of primes multiples is sorted, and that the heads of
		// each channel is p*p and thus come in increasing order.  We use a heap to
		// maintain a pool of channels of multiples and keep draining the heap until
		// the minimum is greater than the head of the next channel, in which case we
		// add that channel into the heap and poll for the next head.
		h := NewPeekChHeap();
		min := 143;
		for {
			ch := multiples(<-primes);
			n := <-ch;
			for min < n {
				out <- min;
				mch := heap.Pop(h).(*PeekCh);
				min = mch.head;
				heap.Push(h, &PeekCh{<-mch.ch, mch.ch});
			}
			for min == n {
				mch := heap.Pop(h).(*PeekCh);
				min = mch.head;
				heap.Push(h, &PeekCh{<-mch.ch, mch.ch});
			}
			out <- n;
			heap.Push(h, &PeekCh{<-ch, ch});
		}
	}();
	return out;
}

// Peekable chan int
type PeekCh struct {
	head	int;
	ch	chan int;
}

// Heap of PeekCh, sorting by heads
type PeekChHeap struct {
	heads	*vector.IntVector;
	chs	*vector.Vector;
}

func NewPeekChHeap() *PeekChHeap {
	h := new(PeekChHeap);
	h.heads = vector.NewIntVector(0);
	h.chs = vector.New(0);
	return h;
}

func (h *PeekChHeap) Push(x interface{}) {
	h.heads.Push(x.(*PeekCh).head);
	h.chs.Push(x.(*PeekCh).ch);
}

func (h *PeekChHeap) Pop() interface{} {
	return &PeekCh{h.heads.Pop(), h.chs.Pop().(chan int)}
}

func (h *PeekChHeap) Len() int	{ return h.heads.Len() }

func (h *PeekChHeap) Less(i, j int) bool	{ return h.heads.Less(i, j) }

func (h *PeekChHeap) Swap(i, j int) {
	h.heads.Swap(i, j);
	h.chs.Swap(i, j);
}

// Return a chan int of primes.
// Attempt to receive more than n primes from the returned channel will deadlock.
func Primes(n int) chan int {
	out := make(chan int, 100);

	primes := make(chan int, n);
	// We need non-blocking send to this, or we'll deadlock.
	// The minimum buffer size must be p(n) where
	// 	p(n) = (number of primes <= n) - (number of primes <= sqrt(n))
	// 	p(10^6) = 78330
	// 	p(10^9) = 50844133
	// 	p(2^31-1) = 105092773

	go func() {
		out <- 2;
		out <- 3;
		out <- 5;
		out <- 7;
		out <- 11;
		primes <- 11;
		composites := mergeMultiples(primes);
		candidates := coprime2357();
		p := <-candidates;
		for {
			c := <-composites;
			for p < c {
				primes <- p;
				out <- p;
				p = <-candidates;
			}
			if p == c {
				p = <-candidates
			}
		}
	}();
	return out;
}

// Return a chan int of primes.
// Attempt to receive primes >= n from the returned channel will deadlock.
func Sieve(n int) chan int	{ return Primes(estimateP(n)) }

// Overestimate p(n) for all n <= 2^31-1 where
// p(n) = (number of primes <= n) - (number of primes <= sqrt(n))
func estimateP(n int) int {
	x := float64(n);
	return int(x/math.Log(x) + math.Pow(x, 0.72505));
}

func main() {
	flag.Parse();
	n, err := strconv.Atoi(flag.Arg(0));
	if err != nil {
		fmt.Fprintln(os.Stderr, "bad argument");
		os.Exit(1);
	}
	runtime.GOMAXPROCS(*nCPU);
	if *nth {
		primes := Primes(n);
		for i := 1; i < n; i++ {
			<-primes
		}
		fmt.Println(<-primes);
	} else {
		primes := Sieve(n);
		for {
			p := <-primes;
			if p <= n {
				fmt.Println(p)
			} else {
				return
			}
		}
	}
}
