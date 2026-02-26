package main

import "time"

var dem = 0

func main() {
	time.Sleep(1 * time.Second)
	println("started")
	go periodic()
	time.Sleep(5 * time.Second)

}

func periodic() {
	for {
		println("tick:", dem)
		dem++
		time.Sleep(1 * time.Second)
	}
}
