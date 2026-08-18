package main

import (
	"testing"

	"github.com/bryant-w/hobot-code/sdk/go/hobot"
)

func TestBPUDataStructures(t *testing.T) {
	req := hobot.BPUBenchmarkRequest{
		ModelPath:   "/root/yolov8_640x640_nv12.bin",
		CoreID:      1,
		FrameCount:  200,
		ThreadCount: 2,
	}
	if req.ModelPath == "" || req.CoreID != 1 {
		t.Fatalf("unexpected request: %+v", req)
	}

	res := hobot.BPUBenchmarkResult{
		ModelPath:        req.ModelPath,
		ModelName:        "yolov8n_640x640_nv12",
		CoreID:           1,
		ThreadCount:      2,
		FrameCount:       200,
		FPS:              155.4,
		AverageLatencyMs: 12.8,
		MinLatencyMs:     6.1,
		MaxLatencyMs:     14.2,
	}

	if res.FPS < 150 {
		t.Errorf("expected FPS > 150, got %f", res.FPS)
	}
}
