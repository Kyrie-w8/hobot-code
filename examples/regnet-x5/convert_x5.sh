#!/bin/sh
set -eu

image='ccr-29eug8s3-pub.cnc.bj.baidubce.com/aitools/ai_toolchain_ubuntu_20_x5_gpu:v1.2.8'
image_digest='sha256:bb706c129bafb4d36d2c338c957369cc5fdb51e0eb65ad5c927b3fda4023ad3f'
onnx_sha='2ae92acad8c92d58adc838ae86575f14e39b1dca33abead412e11181316d9a98'
config_sha='027a4ad4e91b9abf1ab74b773577bfb0b8d85f63b7ed1c27da9d74dadc6a94a3'
calibration_sha='24689306bfe0945286c45b9d8f00a178f1d0a6504b2f0da5e22e31950eede7ea'

usage() {
  printf 'Usage: %s INPUT_DIR OUTPUT_DIR\n       %s --verify INPUT_DIR\n' "$0" "$0" >&2
  exit 2
}

verify_only=0
if [ "$#" -eq 2 ] && [ "$1" = '--verify' ]; then
  verify_only=1
  input=$(realpath "$2")
elif [ "$#" -eq 2 ]; then
  input=$(realpath "$1")
  output=$2
else
  usage
fi
[ -d "$input" ] || usage

[ "$(sha256sum "$input/regnet_x_400mf_224.onnx" | awk '{print $1}')" = "$onnx_sha" ] || {
  printf 'Source ONNX SHA-256 mismatch.\n' >&2; exit 1;
}
[ "$(sha256sum "$input/ptq.yaml" | awk '{print $1}')" = "$config_sha" ] || {
  printf 'PTQ config SHA-256 mismatch.\n' >&2; exit 1;
}
actual_calibration_sha=$(
  cd "$input/data_imagenette/calibration"
  find . -type f -print0 |
    sort -z |
    xargs -0 sha256sum |
    sha256sum |
    awk '{print $1}'
)
[ "$actual_calibration_sha" = "$calibration_sha" ] || {
  printf 'Calibration file-set SHA-256 mismatch.\n' >&2; exit 1;
}
[ "$verify_only" -eq 0 ] || {
  printf 'Verified RegNet X5 conversion input: %s\n' "$input"
  exit 0
}

[ ! -L "$output" ] || { printf 'Output cannot be a symbolic link: %s\n' "$output" >&2; exit 1; }
mkdir -p "$output"
output=$(realpath "$output")
actual_image_digest=$(docker image inspect "$image" --format '{{.Id}}')
[ "$actual_image_digest" = "$image_digest" ] || {
  printf 'OpenExplorer image digest mismatch: %s\n' "$actual_image_digest" >&2; exit 1;
}

docker run --rm \
  --network none \
  --user "$(id -u):$(id -g)" \
  --entrypoint /bin/bash \
  --mount "type=bind,src=$input,dst=/input,readonly" \
  --mount "type=bind,src=$output,dst=/workspace" \
  "$image" -lc '
    set -euo pipefail
    cp /input/ptq.yaml /workspace/ptq.yaml
    sed -i "s#\./regnet_x_400mf_224.onnx#/input/regnet_x_400mf_224.onnx#" /workspace/ptq.yaml
    sed -i "s#\./data_imagenette/calibration/featuremap#/input/data_imagenette/calibration/featuremap#" /workspace/ptq.yaml
    cd /workspace
    hb_mapper makertbin --config ptq.yaml --model-type onnx 2>&1 | tee conversion.log
  '

model="$output/ptq_output/regnet_x_400mf_224_rgb_x5.bin"
[ -f "$model" ] || { printf 'Converted X5 model is missing.\n' >&2; exit 1; }
sha256sum "$model" | tee "$output/artifact.sha256"
printf 'Converted model: %s\n' "$model"
