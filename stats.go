package main

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Stats struct {
	TotalLinks  int `json:"total_links"`
	TotalImages int `json:"total_images"`
	mu          sync.RWMutex
}

var globalStats = &Stats{}

const statsFile = "stats.json"

var (
	statsSaveRequests = make(chan struct{}, 1)
	statsWriterOnce   sync.Once
)

func loadStats() {
	globalStats.mu.Lock()
	defer globalStats.mu.Unlock()

	data, err := os.ReadFile(statsFile)
	if err == nil {
		json.Unmarshal(data, globalStats)
	}
}

func saveStats() {
	globalStats.mu.RLock()
	snapshot := struct {
		TotalLinks  int `json:"total_links"`
		TotalImages int `json:"total_images"`
	}{globalStats.TotalLinks, globalStats.TotalImages}
	globalStats.mu.RUnlock()

	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		log.Printf("serialize stats: %v", err)
		return
	}

	tmp, err := os.CreateTemp(filepath.Dir(statsFile), ".stats-*")
	if err != nil {
		log.Printf("create stats temp file: %v", err)
		return
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		log.Printf("write stats: %v", err)
		return
	}
	if err := tmp.Close(); err != nil {
		log.Printf("close stats file: %v", err)
		return
	}
	if err := os.Chmod(tmpName, 0644); err != nil {
		log.Printf("set stats permissions: %v", err)
		return
	}
	if err := os.Rename(tmpName, statsFile); err != nil {
		log.Printf("replace stats file: %v", err)
	}
}

func addStats(links, images int) {
	globalStats.mu.Lock()
	globalStats.TotalLinks += links
	globalStats.TotalImages += images
	globalStats.mu.Unlock()

	statsWriterOnce.Do(func() { go statsSaveLoop() })
	select {
	case statsSaveRequests <- struct{}{}:
	default:
	}
}

func statsSaveLoop() {
	for range statsSaveRequests {
		timer := time.NewTimer(500 * time.Millisecond)
	coalesce:
		for {
			select {
			case <-statsSaveRequests:
			case <-timer.C:
				break coalesce
			}
		}
		saveStats()
	}
}
