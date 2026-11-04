#!/usr/bin/env bash
# Build on Ubuntu 24.04 using its AArch64 cross sysroot.
set -euo pipefail
if [[ $# != 1 ]]; then echo "usage: $0 OUTPUT.tar.gz" >&2; exit 2; fi
out=$(realpath -m -- "$1")
src=$(cd "$(dirname "$0")" && pwd)
cache=${ASR_CACHE:-"${XDG_CACHE_HOME:-$HOME/.cache}/hey-muse-asr"}
mkdir -p "$cache"
work=$(mktemp -d)
archive=$(mktemp -- "$out.XXXXXX")
trap 'rm -rf -- "$work"; rm -f -- "$archive"' EXIT
root=$work/echo-asr
mkdir -p "$root"/{lib,glibc,models,licenses}
release=https://github.com/k2-fsa/sherpa-onnx/releases/download
runtime=sherpa-onnx-v2.0.1-linux-aarch64-shared-cpu
fetch() {
    local name=$1 url=$2 expected=$3
    if [[ ! -f "$cache/$name" ]]; then
        curl --fail --location --retry 2 "$url" -o "$work/download"
        echo "$expected  $work/download" | sha256sum --check --status
        mv -- "$work/download" "$cache/$name"
    fi
    echo "$expected  $cache/$name" | sha256sum --check
}
fetch "$runtime.tar.bz2" "$release/v2.0.1/$runtime.tar.bz2" a8fc9fb3940d1aa3f83136767742b05068f00253f560d53c4f5aeb75ab4cb350
fetch c-api.h https://raw.githubusercontent.com/k2-fsa/sherpa-onnx/v2.0.1/sherpa-onnx/c-api/c-api.h 5698f38ba09ad2bdf085c56a6cbbd7741acf15e25f4ce5c7097b9702cadf437f
fetch silero_vad.onnx "$release/asr-models/silero_vad.onnx" 9e2449e1087496d8d4caba907f23e0bd3f78d91fa552479bb9c23ac09cbb1fd6
cp "$cache/silero_vad.onnx" "$root/"
tar -xjf "$cache/$runtime.tar.bz2" -C "$work" "$runtime/lib"
cp -L "$work/$runtime/lib/"{libsherpa-onnx-c-api.so,libonnxruntime.so} "$root/lib/"
for lib in ld-linux-aarch64.so.1 libc.so.6 libgcc_s.so.1 libstdc++.so.6 libm.so.6 libdl.so.2 libpthread.so.0 librt.so.1; do
    cp -L "/usr/aarch64-linux-gnu/lib/$lib" "$root/glibc/"
done
for pkg in libc6-arm64-cross libgcc-s1-arm64-cross libstdc++6-arm64-cross; do
    cp -L "/usr/share/doc/$pkg/copyright" "$root/licenses/$pkg-copyright"
done
cp "$src"/licenses/* "$root/licenses/"

for size in tiny base; do
    model=sherpa-onnx-moonshine-$size-ja-quantized-2026-02-27
    case $size in
        tiny) hash=880305c9a6c33572ab269ff9731977ea42ca34c8cffcdd5d99558a9ea2b47cc2 ;;
        base) hash=c66eb87d229a52d43bbe3b7c52a1d23f6208c0fd7c2e81802ecbda83b192798a ;;
    esac
    fetch "$model.tar.bz2" "$release/asr-models/$model.tar.bz2" "$hash"
    mkdir "$root/models/$size"
    tar -xjf "$cache/$model.tar.bz2" -C "$root/models/$size" --strip-components=1 \
        "$model/encoder_model.ort" "$model/decoder_model_merged.ort" "$model/tokens.txt" "$model/LICENSE"
done

(cd "$src/server" && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -o "$root/echo-asr-server" .)
for program in asr-worker vad-service; do
    source=worker
    [[ $program != vad-service ]] || source=vad-service
    aarch64-linux-gnu-gcc -O2 -Wall -I"$cache" "$src/native/$source.c" -o "$root/$program" \
        -L"$root/lib" -Wl,-rpath-link,"$root/lib" -lsherpa-onnx-c-api -lm
done
install -m 755 "$src/select-model.sh" "$root/select-model"
tar -czf "$archive" -C "$work" echo-asr
mv -- "$archive" "$out"
echo "Built $out"
