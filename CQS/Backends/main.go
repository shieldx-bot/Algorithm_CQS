package main

import (
	"fmt"
	"sort"
)

var (
	A float64 = 0.8
	B float64 = 0.1
	C float64 = 0.1
	e float64 = 0.000001
)

func calculatePnew(Pold, Pnew float64, freeQueue, totalQueue int, Ra, Ramax float64) float64 {

	var CP float64
	if Pold == 0 {
		Pold = 1
	}

	Pnew = Pold*0.7 + Pnew*0.3
	CP = (Pnew - Pold) / (Pold + e)

	var QP float64
	QP = 1.0 - float64(freeQueue)/float64(totalQueue)

	var SR = Ra / Ramax
	var PCT float64
	PCT = (A * CP) + (B * QP) - (C * SR)

	return PCT

}
func main() {

	type VPStype struct {
		IP         string
		FreeQueue  int
		TotalQueue int
		Ra         float64
		Ramax      float64
	}
	type Clienttype struct {
		Pold float64
		Pnew float64
	}

	Cients := []Clienttype{
		{Pold: 10, Pnew: 12},
	}

	VPS := []VPStype{
		{IP: "1", FreeQueue: 10, TotalQueue: 100, Ra: 100, Ramax: 120},
		{IP: "2", FreeQueue: 70, TotalQueue: 100, Ra: 90, Ramax: 120},
		{IP: "3", FreeQueue: 60, TotalQueue: 100, Ra: 80, Ramax: 120},
	}
	_ = VPS
	type ResultType struct {
		ClientIndex int
		VPIndex     int
		PCT         float64
	}
	var Results []ResultType

	for i, v := range Cients {

		for j, u := range VPS {
			pct := calculatePnew(v.Pold, v.Pnew, u.FreeQueue, u.TotalQueue, u.Ra, u.Ramax)
			Results = append(Results, ResultType{ClientIndex: i, VPIndex: j, PCT: pct})
		}
		sort.Slice(Results, func(i, j int) bool {
			return Results[i].PCT < Results[j].PCT
		})
		best := Results[0]
		fmt.Printf("\n Best VPS for Client %d is VPS %d with PCT = %f \n", best.ClientIndex, best.VPIndex, best.PCT)
		Results = nil
	}

}
