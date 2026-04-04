package main

import (
	"encoding/json"
	"os"
	"sync"
)

type Stats struct {
	TotalLinks  int `json:"total_links"`
	TotalImages int `json:"total_images"`
	mu          sync.Mutex
}

var globalStats = &Stats{}

const statsFile = "stats.json"

func loadStats() {
	globalStats.mu.Lock()
	defer globalStats.mu.Unlock()

	data, err := os.ReadFile(statsFile)
	if err == nil {
		json.Unmarshal(data, globalStats)
	}
}

func saveStats() {
	globalStats.mu.Lock()
	defer globalStats.mu.Unlock()

	data, _ := json.MarshalIndent(globalStats, "", "  ")
	os.WriteFile(statsFile, data, 0644)
}

func addStats(links, images int) {
	globalStats.mu.Lock()
	globalStats.TotalLinks += links
	globalStats.TotalImages += images
	globalStats.mu.Unlock()
	saveStats()
}
