package hobot

import (
	"testing"
)

const sampleModelInfoOutput = `
I0000 00:00:00.000000 3807339 vlog_is_on.cc:197] RAW: Set VLOG level for "*" to 3
[BPU_PLAT]BPU Platform Version(1.3.6)! soc info(x5)
[HBRT] set log level as 0. version = 3.15.55.0
[DNN] Runtime version = 1.24.5_(3.15.55 HBRT)
[A][DNN][packed_model.cpp:247][Model](2026-08-18,01:27:41.818.304) [HorizonRT] The model builder version = 1.23.6
Load model to DDR cost 379.393ms.
This model file has 1 model:
[yolov8n_640x640_nv12]	
---------------------------------------------------------------------
[model name]: yolov8n_640x640_nv12

input[0]: 
name: images
input source: HB_DNN_INPUT_FROM_PYRAMID
valid shape: (1,3,640,640,)
aligned shape: (1,3,640,640,)
aligned byte size: 614400
tensor type: HB_DNN_IMG_TYPE_NV12
tensor layout: HB_DNN_LAYOUT_NCHW
quanti type: NONE
stride: (0,0,0,0,)

output[0]: 
name: output0
valid shape: (1,80,80,80,)
aligned shape: (1,80,80,80,)
aligned byte size: 2048000
tensor type: HB_DNN_TENSOR_TYPE_F32
tensor layout: HB_DNN_LAYOUT_NHWC
quanti type: NONE
stride: (2048000,25600,320,4,)

output[1]: 
name: 318
valid shape: (1,80,80,64,)
aligned shape: (1,80,80,64,)
aligned byte size: 1638400
tensor type: HB_DNN_TENSOR_TYPE_S32
tensor layout: HB_DNN_LAYOUT_NHWC
quanti type: SCALE
stride: (1638400,20480,256,4,)
quantizeAxis: 3
`

const samplePerfOutput = `
I0000 00:00:00.000000 3807400 vlog_is_on.cc:197] RAW: Set VLOG level for "*" to 3
[BPU_PLAT]BPU Platform Version(1.3.6)! soc info(x5)
Load model to DDR cost 174.773ms.
Frame count: 200,  Thread Average: 12.428570 ms,  thread max latency: 13.361000 ms,  thread min latency: 6.258000 ms,  FPS: 80.279182

Running condition:
  Thread number is: 1
  Frame count   is: 200
  Program run time: 2491.783000 ms
Perf result:
  Frame totally latency is: 2485.713867 ms
  Average    latency    is: 12.428570 ms
  Frame      rate       is: 80.263811 FPS
`

func TestParseBPUModelInfo(t *testing.T) {
	info, err := ParseBPUModelInfo(sampleModelInfoOutput, "/root/yolov8_640x640_nv12.bin")
	if err != nil {
		t.Fatalf("ParseBPUModelInfo failed: %v", err)
	}

	if info.TargetSoC != "x5" {
		t.Errorf("expected TargetSoC=x5, got %s", info.TargetSoC)
	}
	if info.ModelName != "yolov8n_640x640_nv12" {
		t.Errorf("expected ModelName=yolov8n_640x640_nv12, got %s", info.ModelName)
	}
	if info.LoadDDRCostMs != 379.393 {
		t.Errorf("expected LoadDDRCostMs=379.393, got %f", info.LoadDDRCostMs)
	}
	if len(info.Inputs) != 1 {
		t.Fatalf("expected 1 input, got %d", len(info.Inputs))
	}
	if info.Inputs[0].Name != "images" || info.Inputs[0].TensorType != "HB_DNN_IMG_TYPE_NV12" {
		t.Errorf("unexpected input tensor: %+v", info.Inputs[0])
	}
	if len(info.Outputs) != 2 {
		t.Fatalf("expected 2 outputs, got %d", len(info.Outputs))
	}
	if info.Outputs[1].QuantiType != "SCALE" || info.Outputs[1].QuantizeAxis != 3 {
		t.Errorf("unexpected output tensor: %+v", info.Outputs[1])
	}
}

func TestParseBPUBenchmarkResult(t *testing.T) {
	req := BPUBenchmarkRequest{
		ModelPath:   "/root/yolov8_640x640_nv12.bin",
		CoreID:      0,
		FrameCount:  200,
		ThreadCount: 1,
	}
	res, err := ParseBPUBenchmarkResult(samplePerfOutput, req)
	if err != nil {
		t.Fatalf("ParseBPUBenchmarkResult failed: %v", err)
	}

	if res.FPS < 80.0 || res.FPS > 81.0 {
		t.Errorf("expected FPS around 80.26, got %f", res.FPS)
	}
	if res.AverageLatencyMs < 12.0 || res.AverageLatencyMs > 13.0 {
		t.Errorf("expected AverageLatencyMs around 12.42, got %f", res.AverageLatencyMs)
	}
	if res.MinLatencyMs != 6.258 {
		t.Errorf("expected MinLatencyMs=6.258, got %f", res.MinLatencyMs)
	}
	if res.MaxLatencyMs != 13.361 {
		t.Errorf("expected MaxLatencyMs=13.361, got %f", res.MaxLatencyMs)
	}
}
