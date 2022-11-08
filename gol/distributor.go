package gol

import (
	"fmt"
	"sync"
	"time"

	"uk.ac.bris.cs/gameoflife/util"
)

type distributorChannels struct {
	events     chan<- Event
	keyPresses <-chan rune
	ioCommand  chan<- ioCommand
	ioIdle     <-chan bool
	ioFilename chan<- string
	ioOutput   chan<- uint8
	ioInput    <-chan uint8
}

//Works with io to output the Pgm
func outputPgm(matrix [][]byte, turn int, p Params, c distributorChannels) {
	filename := fmt.Sprintf("%dx%dx%d", p.ImageHeight, p.ImageWidth, turn)
	c.ioCommand <- ioOutput
	c.ioFilename <- filename
	for y := 0; y < p.ImageHeight; y++ {
		for x := 0; x < p.ImageWidth; x++ {
			val := matrix[y][x]
			c.ioOutput <- val
		}
	}
}

//Saves current state as Pgm
func save(matrix [][]byte, turn int, p Params, c distributorChannels) [][]byte {
	outputPgm(matrix, turn, p, c)
	matrix = nextState(p, matrix, p.Threads, turn, c)
	c.events <- TurnComplete{turn}
	return matrix
}

//Saves current state as a Pgm and quits the window
func quit(matrix [][]byte, turn int, p Params, c distributorChannels) {
	outputPgm(matrix, turn, p, c)
	alive := getAlive(matrix, p)
	c.events <- FinalTurnComplete{turn, alive}
}

//Ticker function
func tick(c distributorChannels, tock chan<- bool, turnChan <-chan int, matrixChan <-chan [][]byte, p Params) {
	ticker := time.NewTicker(2 * time.Second)
	go func() {
		for range ticker.C {
			tock <- true
			turn := <-turnChan
			matrix := <-matrixChan
			alive := getAlive(matrix, p)
			c.events <- AliveCellsCount{turn, len(alive)}
		}
	}()
}

//Given a marix, returns the alive cells
func getAlive(matrix [][]byte, p Params) []util.Cell {
	var alive []util.Cell
	var cell util.Cell
	for y := 0; y < p.ImageHeight; y++ {
		for x := 0; x < p.ImageWidth; x++ {
			if matrix[y][x] == 255 {
				cell.X = x
				cell.Y = y
				alive = append(alive, cell)
			}
		}
	}
	return alive
}

//Executes runtime events such as keypresses, tickers, and turns
func execute(matrix [][]byte, p Params, c distributorChannels) ([][]byte, int) {
	tock := make(chan bool)
	turnChan := make(chan int)
	matrixChan := make(chan [][]byte)
	threads := p.Threads
	turn := 0

	go tick(c, tock, turnChan, matrixChan, p)

	for turn = 0; turn < p.Turns; turn++ {
		select {
		case <-tock:
			turnChan <- turn
			matrixChan <- matrix
			matrix = nextState(p, matrix, threads, turn, c)
			c.events <- TurnComplete{turn}
		case key := <-c.keyPresses:
			switch key {
			case 'p':
				fmt.Println("Turn:", turn)
				key2 := <-c.keyPresses
				switch key2 {
				case 'p':
					matrix = nextState(p, matrix, threads, turn, c)
					c.events <- TurnComplete{turn}
				case 's':
					matrix = save(matrix, turn, p, c)
				case 'q':
					quit(matrix, turn, p, c)
				}
			case 's':
				matrix = save(matrix, turn, p, c)
			case 'q':
				quit(matrix, turn, p, c)
			}
		default:
			matrix = nextState(p, matrix, threads, turn, c)
			c.events <- TurnComplete{turn}
		}
	}
	return matrix, turn
}

// distributor divides the work between workers and interacts with other goroutines.
func distributor(p Params, c distributorChannels) {
	turn := 0

	//counter := 0
	//var wg sync.WaitGroup

	//Create a 2D slice to store the world.
	ht := p.ImageHeight
	wd := p.ImageWidth

	matrix := make([][]byte, ht)
	for i := range matrix {
		matrix[i] = make([]byte, wd)
	}

	filename := fmt.Sprintf("%dx%d", ht, wd)

	c.ioCommand <- ioInput
	c.ioFilename <- filename

	for y := 0; y < ht; y++ {
		for x := 0; x < wd; x++ {
			var cell util.Cell
			cell.Y = y
			cell.X = x
			b := <-c.ioInput
			matrix[y][x] = b
			if b == 255 {
				c.events <- CellFlipped{0, cell}
			}
		}
	}

	//Execute all turns of the Game of Life.
	matrix, turn = execute(matrix, p, c)

	outputPgm(matrix, turn, p, c)

	//Report the final state using FinalTurnCompleteEvent.
	alive := getAlive(matrix, p)
	c.events <- FinalTurnComplete{turn, alive}

	// Make sure that the Io has finished any output before exiting.
	c.ioCommand <- ioCheckIdle
	<-c.ioIdle

	c.events <- StateChange{turn, Quitting}

	// Close the channel to stop the SDL goroutine gracefully. Removing may cause deadlock.
	close(c.events)
}

//Makes the next state of the matrix at each turn
func nextState(p Params, matrix [][]byte, threads, turn int, c distributorChannels) [][]byte {
	var next [][]byte

	ht := p.ImageHeight
	wd := p.ImageWidth
	thrd := p.Threads

	if thrd == 1 {
		next = game(0, ht, 0, wd, matrix, p, c, turn)
	} else {
		var chans []chan [][]byte
		var split []int
		var wg sync.WaitGroup

		//splitting height into equal(ish) parts
		for i := 0; i < thrd; i++ {
			split = append(split, ht/thrd)
		}
		for i := 0; i < (ht % thrd); i++ {
			split[i] += 1
		}
		split = append([]int{0}, split...)

		for i := 0; i < len(split)-1; i++ {
			split[i+1] += split[i]
		}

		//making the workers, recieving and remaking the matrix
		for i := 0; i < thrd; i++ {
			wg.Add(1)
			temp := make(chan [][]byte)
			chans = append(chans, temp)
			i := i
			go func() {
				wg.Done()
				worker(split[i], split[i+1], 0, wd, matrix, chans[i], p, c, turn)
			}()
		}
		wg.Wait()

		for i := 0; i < len(chans); i++ {
			parts := <-chans[i]
			next = append(next, parts...)
		}
	}
	return next
}

//executes gol using threads
func worker(startY, endY, startX, endX int, matrix [][]byte, out chan<- [][]byte, p Params, c distributorChannels, turn int) {
	filtered := game(startY, endY, startX, endX, matrix, p, c, turn)
	out <- filtered
}

//Calculates changes in given matrix
func game(startY, endY, startX, endX int, matrix [][]byte, p Params, c distributorChannels, turn int) [][]byte {
	ht := endY - startY
	wd := endX - startX
	imgHt := p.ImageHeight

	newWorld := make([][]byte, ht)
	for i := range newWorld {
		newWorld[i] = make([]byte, wd)
	}

	for y := startY; y < endY; y++ {
		for x := 0; x < wd; x++ {
			var cell util.Cell
			cell.Y = y
			cell.X = x

			//find out how many alive neighbours
			sum := (matrix[(y+imgHt-1)%(imgHt)][(x+wd-1)%wd] / 255) + (matrix[(y+imgHt-1)%(imgHt)][(x+wd)%wd] / 255) + (matrix[(y+imgHt-1)%(imgHt)][(x+wd+1)%wd] / 255) +
				(matrix[(y+imgHt)%(imgHt)][(x+wd-1)%wd] / 255) + (matrix[(y+imgHt)%(imgHt)][(x+wd+1)%wd] / 255) +
				(matrix[(y+imgHt+1)%(imgHt)][(x+wd-1)%wd] / 255) + (matrix[(y+imgHt+1)%(imgHt)][(x+wd)%wd] / 255) + (matrix[(y+imgHt+1)%(imgHt)][(x+wd+1)%wd] / 255)

			//update the matrix
			if matrix[y][x] == 255 {
				if sum < 2 {
					newWorld[y-startY][x] = 0
					c.events <- CellFlipped{turn, cell}
				} else if sum == 2 || sum == 3 {
					newWorld[y-startY][x] = 255
				} else {
					newWorld[y-startY][x] = 0
					c.events <- CellFlipped{turn, cell}
				}
			} else {
				if sum == 3 {
					newWorld[y-startY][x] = 255
					c.events <- CellFlipped{turn, cell}
				} else {
					newWorld[y-startY][x] = 0
				}
			}
		}
	}
	return newWorld
}
