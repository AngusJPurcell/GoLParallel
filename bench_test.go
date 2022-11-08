package main

import (
	"fmt"
	"testing"

	"uk.ac.bris.cs/gameoflife/gol"
	"uk.ac.bris.cs/gameoflife/util"
)

// Benchmark applies the filter to the ship.png b.N times.
// The time taken is carefully measured by go.
// The b.N  repetition is needed because benchmark results are not always constant.
func BenchmarkFilter(b *testing.B) {
	tests := []gol.Params{
		{ImageWidth: 16, ImageHeight: 16},
		{ImageWidth: 64, ImageHeight: 64},
		{ImageWidth: 512, ImageHeight: 512},
	}

	for _, p := range tests {
		for _, turns := range []int{0, 1, 100} {
			p.Turns = turns
			// Use a for-loop to run 5 sub-benchmarks, with 1, 2, 4, 8 and 16 workers.
			for threads := 1; threads <= 16; threads++ {
				p.Threads = threads
				//testName := fmt.Sprintf("%dx%dx%d-%d", p.ImageWidth, p.ImageHeight, p.Turns, p.Threads)
				b.Run(fmt.Sprintf("%d_workers", threads), func(b *testing.B) {
					for i := 0; i < b.N; i++ {
						b.StartTimer()
						events := make(chan gol.Event)
						go gol.Run(p, events, nil)
						var cells []util.Cell
						for event := range events {
							switch e := event.(type) {
							case gol.FinalTurnComplete:
								cells = e.Alive
								b.StopTimer()

							}

						}
						cells = append(cells, cells...)
					}
				})
			}
		}
	}

}
