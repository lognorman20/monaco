package main

import "fmt"

func formatSOL(lamports uint64) string {
	return fmt.Sprintf("%.9f", float64(lamports)/1e9)
}

func formatUSDC(micros uint64) string {
	return fmt.Sprintf("%.2f", float64(micros)/1e6)
}
