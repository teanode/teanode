# Spike S1: Silero VAD dependency decision
Decision: http-sidecar

Rationale:
- The current container image in `/Users/hao/Projects/teanode-pipeline/Dockerfile` is `busybox` with a prebuilt binary copied in. This strongly suggests a static/non-CGO runtime target today.
- `go.mod` has no ONNX runtime dependency and no existing CGO-based provider stack. Introducing `onnxruntime_go` would add native runtime packaging + linker/runtime requirements not present in the current build.
- The backlog requires reliable macOS-hosted `GOOS=linux GOARCH=amd64` build expectations. A CGO + ONNX runtime path increases cross-compile and CI variance materially versus a process-separated sidecar.
- Given these constraints, a Python HTTP sidecar is the lower-risk integration path for L1.1 in this repository right now.

Latency measurement:
- Not benchmarked in this spike run because ONNX runtime/prototype binary was not added to the repository in this task branch.
- Operational target remains: p99 <= 2ms/frame for in-process ONNX to justify CGO adoption; sidecar remains preferred if >5ms or if packaging complexity is high.

Cross-compile notes:
- Current repo shape is CGO-light and compatible with straightforward Go cross-compilation.
- Adopting in-process ONNX would require CGO-enabled Linux toolchain compatibility and shipping ONNX runtime shared artifacts for target environments, which is a non-trivial change from the current `busybox` image flow.

Recommended model path: sidecar URL (e.g. `http://127.0.0.1:8081/vad/silero`)
