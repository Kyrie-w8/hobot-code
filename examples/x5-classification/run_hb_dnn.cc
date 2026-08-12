// Repeated-inference runner for batch-1 RGB classifiers on the RDK X5 hb_dnn C API.

#include <algorithm>
#include <chrono>
#include <cstdint>
#include <cstring>
#include <fstream>
#include <iomanip>
#include <iostream>
#include <stdexcept>
#include <string>
#include <vector>

#include <dnn/hb_dnn.h>
#include <dnn/hb_sys.h>

namespace {

using Clock = std::chrono::steady_clock;

void check(int32_t status, const char *operation) {
  if (status != 0) {
    throw std::runtime_error(std::string(operation) + " failed: " +
                             std::to_string(status));
  }
}

std::vector<uint8_t> read_file(const std::string &path) {
  std::ifstream source(path, std::ios::binary | std::ios::ate);
  if (!source) throw std::runtime_error("cannot open input: " + path);
  const auto size = source.tellg();
  source.seekg(0);
  std::vector<uint8_t> value(static_cast<size_t>(size));
  if (!source.read(reinterpret_cast<char *>(value.data()), size)) {
    throw std::runtime_error("cannot read input: " + path);
  }
  return value;
}

double milliseconds(Clock::time_point start, Clock::time_point end) {
  return std::chrono::duration<double, std::milli>(end - start).count();
}

double percentile(std::vector<double> values, double fraction) {
  std::sort(values.begin(), values.end());
  const double position = (values.size() - 1) * fraction;
  const size_t lower = static_cast<size_t>(position);
  const size_t upper = std::min(lower + 1, values.size() - 1);
  return values[lower] * (upper - position) + values[upper] * (position - lower);
}

void copy_rgb_nhwc(const std::vector<uint8_t> &source, hbDNNTensor *tensor) {
  const auto &valid = tensor->properties.validShape;
  const auto &aligned = tensor->properties.alignedShape;
  if (tensor->properties.tensorType != HB_DNN_IMG_TYPE_RGB ||
      tensor->properties.tensorLayout != HB_DNN_LAYOUT_NHWC ||
      valid.numDimensions != 4 || valid.dimensionSize[0] != 1 ||
      valid.dimensionSize[3] != 3 || aligned.dimensionSize[2] < valid.dimensionSize[2]) {
    throw std::runtime_error("runner requires batch-1 RGB/NHWC input");
  }
  const size_t rows = static_cast<size_t>(valid.dimensionSize[1]);
  const size_t valid_row = static_cast<size_t>(valid.dimensionSize[2]) * 3;
  const size_t aligned_row = static_cast<size_t>(aligned.dimensionSize[2]) * 3;
  if (source.size() != rows * valid_row) {
    throw std::runtime_error("RGB input byte count does not match valid tensor shape");
  }
  std::memset(tensor->sysMem[0].virAddr, 0, tensor->properties.alignedByteSize);
  auto *destination = static_cast<uint8_t *>(tensor->sysMem[0].virAddr);
  for (size_t row = 0; row < rows; ++row) {
    std::memcpy(destination + row * aligned_row, source.data() + row * valid_row,
                valid_row);
  }
}

void free_tensor(hbDNNTensor *tensor) {
  if (tensor->sysMem[0].virAddr != nullptr) hbSysFreeMem(&tensor->sysMem[0]);
}

}  // namespace

int main(int argc, char **argv) {
  if (argc != 5) {
    std::cerr << "Usage: " << argv[0]
              << " MODEL.bin INPUT.rgb WARMUP ITERATIONS\n";
    return 2;
  }
  const int warmup = std::stoi(argv[3]);
  const int iterations = std::stoi(argv[4]);
  if (warmup < 0 || iterations < 1) return 2;

  hbPackedDNNHandle_t packed = nullptr;
  std::vector<hbDNNTensor> outputs;
  hbDNNTensor input{};
  try {
    const char *model_files[] = {argv[1]};
    check(hbDNNInitializeFromFiles(&packed, model_files, 1),
          "hbDNNInitializeFromFiles");
    const char **names = nullptr;
    int32_t model_count = 0;
    check(hbDNNGetModelNameList(&names, &model_count, packed),
          "hbDNNGetModelNameList");
    if (model_count != 1) throw std::runtime_error("expected exactly one model");
    hbDNNHandle_t model = nullptr;
    check(hbDNNGetModelHandle(&model, packed, names[0]), "hbDNNGetModelHandle");

    int32_t input_count = 0;
    int32_t output_count = 0;
    check(hbDNNGetInputCount(&input_count, model), "hbDNNGetInputCount");
    check(hbDNNGetOutputCount(&output_count, model), "hbDNNGetOutputCount");
    if (input_count != 1 || output_count < 1) {
      throw std::runtime_error("runner requires one input and at least one output");
    }
    check(hbDNNGetInputTensorProperties(&input.properties, model, 0),
          "hbDNNGetInputTensorProperties");
    check(hbSysAllocCachedMem(&input.sysMem[0], input.properties.alignedByteSize),
          "hbSysAllocCachedMem(input)");
    outputs.resize(static_cast<size_t>(output_count));
    for (int32_t index = 0; index < output_count; ++index) {
      check(hbDNNGetOutputTensorProperties(&outputs[index].properties, model, index),
            "hbDNNGetOutputTensorProperties");
      check(hbSysAllocCachedMem(&outputs[index].sysMem[0],
                                outputs[index].properties.alignedByteSize),
            "hbSysAllocCachedMem(output)");
    }

    const auto image = read_file(argv[2]);
    std::vector<double> model_latency;
    std::vector<double> end_to_end_latency;
    model_latency.reserve(iterations);
    end_to_end_latency.reserve(iterations);
    hbDNNInferCtrlParam control{};
    HB_DNN_INITIALIZE_INFER_CTRL_PARAM(&control);
    for (int index = -warmup; index < iterations; ++index) {
      const auto end_to_end_start = Clock::now();
      copy_rgb_nhwc(image, &input);
      check(hbSysFlushMem(&input.sysMem[0], HB_SYS_MEM_CACHE_CLEAN),
            "hbSysFlushMem(input)");
      hbDNNTaskHandle_t task = nullptr;
      hbDNNTensor *output_data = outputs.data();
      const auto model_start = Clock::now();
      check(hbDNNInfer(&task, &output_data, &input, model, &control), "hbDNNInfer");
      check(hbDNNWaitTaskDone(task, 0), "hbDNNWaitTaskDone");
      const auto model_end = Clock::now();
      check(hbDNNReleaseTask(task), "hbDNNReleaseTask");
      for (auto &output : outputs) {
        check(hbSysFlushMem(&output.sysMem[0], HB_SYS_MEM_CACHE_INVALIDATE),
              "hbSysFlushMem(output)");
      }
      const auto end_to_end_end = Clock::now();
      if (index >= 0) {
        model_latency.push_back(milliseconds(model_start, model_end));
        end_to_end_latency.push_back(milliseconds(end_to_end_start, end_to_end_end));
      }
    }

    double total = 0;
    for (double value : model_latency) total += value;
    std::cout << std::fixed << std::setprecision(6)
              << "{\n"
              << "  \"schema\": 1,\n"
              << "  \"runtime\": \"hb_dnn " << hbDNNGetVersion() << "\",\n"
              << "  \"warmupIterations\": " << warmup << ",\n"
              << "  \"iterations\": " << iterations << ",\n"
              << "  \"modelP50LatencyMs\": " << percentile(model_latency, 0.50) << ",\n"
              << "  \"modelP95LatencyMs\": " << percentile(model_latency, 0.95) << ",\n"
              << "  \"endToEndP50LatencyMs\": " << percentile(end_to_end_latency, 0.50) << ",\n"
              << "  \"endToEndP95LatencyMs\": " << percentile(end_to_end_latency, 0.95) << ",\n"
              << "  \"throughputFps\": " << (1000.0 * iterations / total) << "\n"
              << "}\n";
  } catch (const std::exception &error) {
    std::cerr << error.what() << '\n';
    for (auto &output : outputs) free_tensor(&output);
    free_tensor(&input);
    if (packed != nullptr) hbDNNRelease(packed);
    return 1;
  }
  for (auto &output : outputs) free_tensor(&output);
  free_tensor(&input);
  if (packed != nullptr) hbDNNRelease(packed);
  return 0;
}
