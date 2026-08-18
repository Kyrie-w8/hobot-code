package hobot

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)


type BPUTensorDesc struct {
	Index        int    `json:"index"`
	Name         string `json:"name"`
	InputSource  string `json:"inputSource,omitempty"`
	ValidShape   string `json:"validShape"`
	AlignedShape string `json:"alignedShape"`
	AlignedBytes int64  `json:"alignedBytes"`
	TensorType   string `json:"tensorType"`
	TensorLayout string `json:"tensorLayout"`
	QuantiType   string `json:"quantiType"`
	Stride       string `json:"stride,omitempty"`
	QuantizeAxis int    `json:"quantizeAxis,omitempty"`
}

type BPUModelInfo struct {
	ModelName          string          `json:"modelName"`
	ModelFile          string          `json:"modelFile"`
	TargetSoC          string          `json:"targetSoc,omitempty"`
	BPUPlatformVersion string          `json:"bpuPlatformVersion,omitempty"`
	HBRTVersion        string          `json:"hbrtVersion,omitempty"`
	DNNVersion         string          `json:"dnnVersion,omitempty"`
	ModelBuilderVer    string          `json:"modelBuilderVersion,omitempty"`
	LoadDDRCostMs      float64         `json:"loadDdrCostMs"`
	Inputs             []BPUTensorDesc `json:"inputs"`
	Outputs            []BPUTensorDesc `json:"outputs"`
	RawOutput          string          `json:"rawOutput,omitempty"`
}

type BPUBenchmarkRequest struct {
	ModelPath   string `json:"modelPath"`
	ModelName   string `json:"modelName,omitempty"`
	CoreID      int    `json:"coreId"`      // 0=all/any, 1=core0, 2=core1
	FrameCount  int    `json:"frameCount"`  // default 200
	ThreadCount int    `json:"threadCount"` // default 1
	InputFile   string `json:"inputFile,omitempty"`
}


type BPUBenchmarkResult struct {
	ModelPath        string    `json:"modelPath"`
	ModelName        string    `json:"modelName"`
	CoreID           int       `json:"coreId"`
	ThreadCount      int       `json:"threadCount"`
	FrameCount       int       `json:"frameCount"`
	FPS              float64   `json:"fps"`
	AverageLatencyMs float64   `json:"averageLatencyMs"`
	MinLatencyMs     float64   `json:"minLatencyMs"`
	MaxLatencyMs     float64   `json:"maxLatencyMs"`
	ProgramRunTimeMs float64   `json:"programRunTimeMs"`
	TotalLatencyMs   float64   `json:"totalLatencyMs"`
	CapturedAt       time.Time `json:"capturedAt"`
	RawOutput        string    `json:"rawOutput,omitempty"`
}

var (
	reSocInfo         = regexp.MustCompile(`(?i)soc info\(([^)]+)\)`)
	reBpuPlatVer      = regexp.MustCompile(`(?i)BPU Platform Version\(([^)]+)\)`)
	reHbrtVer         = regexp.MustCompile(`(?i)version\s*=\s*([0-9.]+)`)
	reDnnVer          = regexp.MustCompile(`(?i)Runtime version\s*=\s*([^\n\r]+)`)
	reModelBuilderVer = regexp.MustCompile(`(?i)model builder version\s*=\s*([0-9.]+)`)
	reLoadDDR         = regexp.MustCompile(`(?i)Load model to DDR cost\s+([0-9.]+)ms`)
	reModelName       = regexp.MustCompile(`(?i)\[model name\]:\s*([^\n\r]+)`)
	reFrameRate       = regexp.MustCompile(`(?i)Frame\s+rate\s+is:\s*([0-9.]+)\s*FPS`)
	reAvgLatency      = regexp.MustCompile(`(?i)Average\s+latency\s+is:\s*([0-9.]+)\s*ms`)
	reMinLatency      = regexp.MustCompile(`(?i)thread min latency:\s*([0-9.]+)\s*ms`)
	reMaxLatency      = regexp.MustCompile(`(?i)thread max latency:\s*([0-9.]+)\s*ms`)
	reProgramRunTime  = regexp.MustCompile(`(?i)Program run time:\s*([0-9.]+)\s*ms`)
	reTotalLatency    = regexp.MustCompile(`(?i)Frame totally latency is:\s*([0-9.]+)\s*ms`)
	reFallbackFPS     = regexp.MustCompile(`(?i)FPS:\s*([0-9.]+)`)
	reFallbackAvgLat  = regexp.MustCompile(`(?i)Thread Average:\s*([0-9.]+)\s*ms`)
)

func quoteArg(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func ParseBPUModelInfo(raw string, modelPath string) (BPUModelInfo, error) {
	info := BPUModelInfo{
		ModelFile: modelPath,
		RawOutput: raw,
		Inputs:    make([]BPUTensorDesc, 0),
		Outputs:   make([]BPUTensorDesc, 0),
	}

	if match := reSocInfo.FindStringSubmatch(raw); len(match) > 1 {
		info.TargetSoC = strings.TrimSpace(match[1])
	}
	if match := reBpuPlatVer.FindStringSubmatch(raw); len(match) > 1 {
		info.BPUPlatformVersion = strings.TrimSpace(match[1])
	}
	if match := reHbrtVer.FindStringSubmatch(raw); len(match) > 1 {
		info.HBRTVersion = strings.TrimSpace(match[1])
	}
	if match := reDnnVer.FindStringSubmatch(raw); len(match) > 1 {
		info.DNNVersion = strings.TrimSpace(match[1])
	}
	if match := reModelBuilderVer.FindStringSubmatch(raw); len(match) > 1 {
		info.ModelBuilderVer = strings.TrimSpace(match[1])
	}
	if match := reLoadDDR.FindStringSubmatch(raw); len(match) > 1 {
		if val, err := strconv.ParseFloat(match[1], 64); err == nil {
			info.LoadDDRCostMs = val
		}
	}
	if match := reModelName.FindStringSubmatch(raw); len(match) > 1 {
		info.ModelName = strings.TrimSpace(match[1])
	}

	// Parse tensors
	sections := strings.Split(raw, "---------------------------------------------------------------------")
	tensorBlock := raw
	if len(sections) >= 2 {
		tensorBlock = sections[1]
	}

	lines := strings.Split(tensorBlock, "\n")
	var currentTensor *BPUTensorDesc
	var isOutput bool

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		if strings.HasPrefix(trimmed, "input[") {
			if currentTensor != nil {
				if isOutput {
					info.Outputs = append(info.Outputs, *currentTensor)
				} else {
					info.Inputs = append(info.Inputs, *currentTensor)
				}
			}
			idx := 0
			fmt.Sscanf(trimmed, "input[%d]:", &idx)
			currentTensor = &BPUTensorDesc{Index: idx}
			isOutput = false
			continue
		} else if strings.HasPrefix(trimmed, "output[") {
			if currentTensor != nil {
				if isOutput {
					info.Outputs = append(info.Outputs, *currentTensor)
				} else {
					info.Inputs = append(info.Inputs, *currentTensor)
				}
			}
			idx := 0
			fmt.Sscanf(trimmed, "output[%d]:", &idx)
			currentTensor = &BPUTensorDesc{Index: idx}
			isOutput = true
			continue
		}

		if currentTensor == nil {
			continue
		}

		colonIdx := strings.Index(trimmed, ":")
		if colonIdx == -1 {
			continue
		}

		key := strings.TrimSpace(trimmed[:colonIdx])
		val := strings.TrimSpace(trimmed[colonIdx+1:])

		switch key {
		case "name":
			currentTensor.Name = val
		case "input source":
			currentTensor.InputSource = val
		case "valid shape":
			currentTensor.ValidShape = val
		case "aligned shape":
			currentTensor.AlignedShape = val
		case "aligned byte size":
			if b, err := strconv.ParseInt(val, 10, 64); err == nil {
				currentTensor.AlignedBytes = b
			}
		case "tensor type":
			currentTensor.TensorType = val
		case "tensor layout":
			currentTensor.TensorLayout = val
		case "quanti type":
			currentTensor.QuantiType = val
		case "stride":
			currentTensor.Stride = val
		case "quantizeAxis":
			if a, err := strconv.Atoi(val); err == nil {
				currentTensor.QuantizeAxis = a
			}
		}
	}

	if currentTensor != nil {
		if isOutput {
			info.Outputs = append(info.Outputs, *currentTensor)
		} else {
			info.Inputs = append(info.Inputs, *currentTensor)
		}
	}

	return info, nil
}

func ParseBPUBenchmarkResult(raw string, req BPUBenchmarkRequest) (BPUBenchmarkResult, error) {
	res := BPUBenchmarkResult{
		ModelPath:   req.ModelPath,
		CoreID:      req.CoreID,
		ThreadCount: req.ThreadCount,
		FrameCount:  req.FrameCount,
		CapturedAt:  time.Now().UTC(),
		RawOutput:   raw,
	}
	if res.ThreadCount <= 0 {
		res.ThreadCount = 1
	}
	if res.FrameCount <= 0 {
		res.FrameCount = 200
	}

	base := req.ModelPath
	if idx := strings.LastIndex(base, "/"); idx != -1 {
		base = base[idx+1:]
	}
	base = strings.TrimSuffix(base, ".bin")
	base = strings.TrimSuffix(base, ".hbm")
	base = strings.TrimSuffix(base, ".onnx")
	res.ModelName = base

	if match := reFrameRate.FindStringSubmatch(raw); len(match) > 1 {
		if v, err := strconv.ParseFloat(match[1], 64); err == nil {
			res.FPS = v
		}
	} else if match := reFallbackFPS.FindStringSubmatch(raw); len(match) > 1 {
		if v, err := strconv.ParseFloat(match[1], 64); err == nil {
			res.FPS = v
		}
	}

	if match := reAvgLatency.FindStringSubmatch(raw); len(match) > 1 {
		if v, err := strconv.ParseFloat(match[1], 64); err == nil {
			res.AverageLatencyMs = v
		}
	} else if match := reFallbackAvgLat.FindStringSubmatch(raw); len(match) > 1 {
		if v, err := strconv.ParseFloat(match[1], 64); err == nil {
			res.AverageLatencyMs = v
		}
	}

	if match := reMinLatency.FindStringSubmatch(raw); len(match) > 1 {
		if v, err := strconv.ParseFloat(match[1], 64); err == nil {
			res.MinLatencyMs = v
		}
	}
	if match := reMaxLatency.FindStringSubmatch(raw); len(match) > 1 {
		if v, err := strconv.ParseFloat(match[1], 64); err == nil {
			res.MaxLatencyMs = v
		}
	}
	if res.MinLatencyMs == 0 {
		res.MinLatencyMs = res.AverageLatencyMs
	}
	if res.MaxLatencyMs == 0 {
		res.MaxLatencyMs = res.AverageLatencyMs
	}

	if match := reProgramRunTime.FindStringSubmatch(raw); len(match) > 1 {
		if v, err := strconv.ParseFloat(match[1], 64); err == nil {
			res.ProgramRunTimeMs = v
		}
	}
	if match := reTotalLatency.FindStringSubmatch(raw); len(match) > 1 {
		if v, err := strconv.ParseFloat(match[1], 64); err == nil {
			res.TotalLatencyMs = v
		}
	}

	if res.FPS == 0 && res.AverageLatencyMs == 0 {
		return res, fmt.Errorf("could not parse benchmark performance metrics from output: %s", raw)
	}

	return res, nil
}

// InspectBPUModel executes hrt_model_exec model_info on the target board and returns structured info.
func (client *Client) InspectBPUModel(ctx context.Context, modelPath string) (BPUModelInfo, error) {
	cmd := fmt.Sprintf("hrt_model_exec model_info --model_file %s 2>&1", quoteArg(modelPath))
	outputBytes, err := client.runBoardCommand(ctx, cmd, nil)
	output := string(outputBytes)
	if err != nil && !strings.Contains(output, "input[") {
		return BPUModelInfo{}, fmt.Errorf("hrt_model_exec model_info failed: %w (output: %s)", err, output)
	}
	return ParseBPUModelInfo(output, modelPath)
}

// RunBPUBenchmark executes hrt_model_exec perf on the target board and returns benchmark measurements.
func (client *Client) RunBPUBenchmark(ctx context.Context, req BPUBenchmarkRequest) (BPUBenchmarkResult, error) {
	if req.ModelPath == "" {
		return BPUBenchmarkResult{}, fmt.Errorf("model path is required")
	}
	frameCount := req.FrameCount
	if frameCount <= 0 {
		frameCount = 200
	}
	threadCount := req.ThreadCount
	if threadCount <= 0 {
		threadCount = 1
	}

	cmd := fmt.Sprintf("hrt_model_exec perf --model_file %s --frame_count %d --thread_num %d",
		quoteArg(req.ModelPath), frameCount, threadCount)
	if req.ModelName != "" {
		cmd += fmt.Sprintf(" --model_name %s", quoteArg(req.ModelName))
	}
	if req.CoreID > 0 {
		cmd += fmt.Sprintf(" --core_id %d", req.CoreID)
	}
	if req.InputFile != "" {
		cmd += fmt.Sprintf(" --input_file %s", quoteArg(req.InputFile))
	}
	cmd += " 2>&1"


	outputBytes, err := client.runBoardCommand(ctx, cmd, nil)
	output := string(outputBytes)
	if err != nil && !reFrameRate.MatchString(output) && !reFallbackFPS.MatchString(output) && !strings.Contains(output, "FPS") {
		return BPUBenchmarkResult{}, fmt.Errorf("hrt_model_exec perf failed: %w (output: %s)", err, output)
	}

	return ParseBPUBenchmarkResult(output, req)
}


// ListBPUModels finds .bin, .hbm and .onnx model files in the cwd, /root, /opt/hobot/model, /userdata, /tmp, etc.
func (client *Client) ListBPUModels(ctx context.Context, cwd string) ([]string, error) {
	dirs := []string{"/root/models", "/userdata/models", "/root", "/opt/hobot/model/x5/basic", "/opt/hobot/model", "/root/ssd"}
	if cwd != "" && cwd != "/root" {
		dirs = append([]string{cwd}, dirs...)
	}

	// Build find command scanning target directories
	var validDirs []string
	seen := make(map[string]bool)
	for _, d := range dirs {
		if !seen[d] {
			seen[d] = true
			validDirs = append(validDirs, quoteArg(d))
		}
	}

	cmd := fmt.Sprintf("find %s -maxdepth 3 -type f \\( -name '*.bin' -o -name '*.hbm' \\) 2>/dev/null | head -n 40", strings.Join(validDirs, " "))
	outputBytes, err := client.runBoardCommand(ctx, cmd, nil)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(outputBytes)), "\n")
	var models []string
	modelSeen := make(map[string]bool)
	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if trimmed != "" && !modelSeen[trimmed] {
			modelSeen[trimmed] = true
			models = append(models, trimmed)
		}
	}
	return models, nil
}

// DownloadSampleBPUModel downloads a verified standard benchmark model for the given SoC.
func (client *Client) DownloadSampleBPUModel(ctx context.Context, soc string) (string, error) {
	socLower := strings.ToLower(soc)
	targetDir := "/root/models"

	// Prepare target directory
	mkdirCmd := fmt.Sprintf("mkdir -p %s", quoteArg(targetDir))
	if _, err := client.runBoardCommand(ctx, mkdirCmd, nil); err != nil {
		targetDir = "/tmp/models"
		_, _ = client.runBoardCommand(ctx, fmt.Sprintf("mkdir -p %s", quoteArg(targetDir)), nil)
	}

	var targetFile string
	var downloadScript string

	if strings.Contains(socLower, "x5") || strings.Contains(socLower, "bayese") {
		targetFile = fmt.Sprintf("%s/mobilenetv2_224x224_nv12.bin", targetDir)
		downloadScript = fmt.Sprintf(`
if [ -f "/opt/hobot/model/x5/basic/mobilenetv2_224x224_nv12.bin" ]; then
  cp "/opt/hobot/model/x5/basic/mobilenetv2_224x224_nv12.bin" %[1]s
elif [ -f "/app/multimedia_samples/sunrise_camera/Platform/x5/model_zoom/mobilenetv2_224x224_nv12.bin" ]; then
  cp "/app/multimedia_samples/sunrise_camera/Platform/x5/model_zoom/mobilenetv2_224x224_nv12.bin" %[1]s
else
  curl -fsSL -o %[1]s "https://github.com/D-Robotics/rdk_model_zoo/raw/main/demos/Vision/mobilenet_v2/models/mobilenetv2_224x224_nv12.bin" || \
  wget -q -O %[1]s "https://github.com/D-Robotics/rdk_model_zoo/raw/main/demos/Vision/mobilenet_v2/models/mobilenetv2_224x224_nv12.bin"
fi
[ -s %[1]s ] && echo "OK" || echo "FAIL"
`, quoteArg(targetFile))
	} else {
		// S100 / S600 (Nash-E / Nash-M / HBM)
		targetFile = fmt.Sprintf("%s/mobilenetv2_224x224_nv12.hbm", targetDir)
		downloadScript = fmt.Sprintf(`
if [ -f "/root/ssd/Ultralytics_YOLO/yolo26x_cls_nashe_224x224_nv12.hbm" ]; then
  cp "/root/ssd/Ultralytics_YOLO/yolo26x_cls_nashe_224x224_nv12.hbm" %[1]s
elif [ -f "/root/ssd/YOLOv8_LowLatency/yolov8n_dfl_test1_hwcrgb888.hbm" ]; then
  cp "/root/ssd/YOLOv8_LowLatency/yolov8n_dfl_test1_hwcrgb888.hbm" %[1]s
else
  curl -fsSL -o %[1]s "https://github.com/D-Robotics/rdk_model_zoo/raw/main/demos/Vision/mobilenet_v2/models/mobilenetv2_224x224_nashe_nv12.hbm" || \
  wget -q -O %[1]s "https://github.com/D-Robotics/rdk_model_zoo/raw/main/demos/Vision/mobilenet_v2/models/mobilenetv2_224x224_nashe_nv12.hbm"
fi
[ -s %[1]s ] && echo "OK" || echo "FAIL"
`, quoteArg(targetFile))
	}

	outBytes, err := client.runBoardCommand(ctx, downloadScript, nil)
	if err != nil || !strings.Contains(string(outBytes), "OK") {
		return "", fmt.Errorf("failed to deploy standard benchmark model: %v (output: %s)", err, strings.TrimSpace(string(outBytes)))
	}

	return targetFile, nil
}

// UploadBPUModel saves a model file provided as raw base64 data to the board.
func (client *Client) UploadBPUModel(ctx context.Context, filename string, data []byte) (string, error) {
	if len(data) == 0 {
		return "", fmt.Errorf("model data is empty")
	}
	baseName := filepath.Base(filename)
	if baseName == "." || baseName == "/" || baseName == "" {
		baseName = "custom_model.bin"
	}
	targetDir := "/root/models"
	_, _ = client.runBoardCommand(ctx, fmt.Sprintf("mkdir -p %s", quoteArg(targetDir)), nil)
	targetFile := fmt.Sprintf("%s/%s", targetDir, baseName)

	writeCmd := fmt.Sprintf("cat > %s && chmod 644 %s && echo 'OK'", quoteArg(targetFile), quoteArg(targetFile))
	outBytes, err := client.runBoardCommand(ctx, writeCmd, data)
	if err != nil || !strings.Contains(string(outBytes), "OK") {
		return "", fmt.Errorf("failed to write model file: %v (output: %s)", err, strings.TrimSpace(string(outBytes)))
	}
	return targetFile, nil
}

