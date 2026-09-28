# Go Inference Server

A small text-inference service: a Go HTTP server that validates requests, enforces limits and timeouts, and streams generated tokens to clients as Server-Sent Events, in front of an inference worker running [Qwen2.5-0.5B-Instruct](https://huggingface.co/Qwen/Qwen2.5-0.5B-Instruct). Two workers are supported behind the same Go service:

- **Version 1:** a Python worker using Hugging Face `transformers`, one generation per thread, no batching.
- **Version 2:** [vLLM](https://github.com/vllm-project/vllm), with continuous batching, spoken to through its OpenAI-compatible API.

This is a learning project. Version 1 deliberately does no batching so that vLLM can be measured against it; the measurements are in [design_doc.md](design_doc.md) §10–§11, and the short version is in [Benchmark results](#benchmark-results) below. The design doc also has the API contract, error codes, timeouts and worker protocols.

```
                 ┌──────────────┐        ┌────────────────┐        ┌─────────────────────────────┐
Client ──HTTPS──▶│ Nginx (edge) │──HTTP──▶│   Go service   │──HTTP──▶│ worker: Python (v1) or vLLM │──▶ GPU
       ◀──SSE────│  optional    │◀────────│ :8080          │◀────────│ :8000 / :8001               │
                 └──────────────┘        └────────────────┘        └─────────────────────────────┘
```

The Go service owns the public API, validation, request IDs, admission control, timeouts, cancellation and SSE streaming. The worker owns tokenization, model execution and batching. Nginx, when used, owns TLS, per-client rate limits and the request-body limit at the edge.

## Repository layout

| Path | What it is |
|---|---|
| `cmd/server` | The Go inference service (`main.go` wires flags, the worker client and graceful shutdown) |
| `cmd/loadgen` | Load generator for the benchmarks in design doc §10–§11 |
| `internal/api` | HTTP handlers, SSE writer, request-ID middleware, config and timeouts |
| `internal/worker` | The `Generator` interface, the v1 HTTP client, the vLLM client, and a fake worker for tests |
| `worker/server.py` | The version 1 Python worker |
| `deploy/nginx.conf` | Edge proxy configuration (TLS, streaming, rate and body limits) |
| `bench/` | Raw per-request CSVs and GPU logs from the benchmark runs (`v1` = Python worker, `v2`/`v3` = vLLM) |
| `requirements.txt` | Pinned Python dependencies for the v1 worker (CUDA 12.6 build of PyTorch) |
| `design_doc.md` | Design, protocols and benchmark results |

## Prerequisites

- Go 1.26 or newer
- For the v1 worker: Python 3.10 or newer. An NVIDIA GPU is recommended but not required; the worker falls back to CPU.
- For vLLM: Linux. On Windows that means WSL2 with an NVIDIA driver recent enough to expose the GPU inside it (`nvidia-smi` works in the Ubuntu shell).
- For the edge proxy: Nginx (the Windows zip from nginx.org works as-is).

## Setup: version 1 worker

Create a virtual environment and install the Python dependencies:

```bash
python -m venv .venv
```

```bash
# Windows PowerShell
.venv\Scripts\Activate.ps1
# macOS / Linux
source .venv/bin/activate
```

```bash
pip install -r requirements.txt
```

`requirements.txt` pins the CUDA 12.6 build of PyTorch from the PyTorch index. On Windows or Linux without an NVIDIA GPU it still installs and runs on CPU. On macOS that wheel does not exist, so install plain `torch` first and then the rest.

The model (~1 GB) is downloaded from Hugging Face into the local cache the first time the worker starts. No account or token is needed.

Check that PyTorch can see your GPU:

```bash
python -c "import torch; print(torch.__version__, torch.cuda.is_available())"
```

A CUDA build prints a version like `2.14.0+cu126` and `True`. A version ending in `+cpu` means the CPU-only build was installed.

## Setup: vLLM worker (WSL2)

Inside the Ubuntu shell (Python 3.9+ required, so Ubuntu 22.04 or newer):

```bash
python3 -m venv ~/vllm-env && source ~/vllm-env/bin/activate && pip install vllm
```

This is a multi-GB download. vLLM has its own Hugging Face cache inside WSL, so the model is downloaded again on first start.

## Running

### 1. Start a worker

**Version 1 (Python):**

```bash
python worker/server.py
```

It loads the model, then listens on port 8000. Until loading finishes, connections are refused and the Go service reports `503 worker_unavailable`. The number of generations it runs at once is set with an environment variable, defaults to 2, and must equal the Go service's `-max-active`:

```bash
# PowerShell
$env:MAX_CONCURRENT = "1"; python worker/server.py
```

The benchmark showed 1 is the right value for this worker (see below).

**Version 2 (vLLM), in the Ubuntu shell:**

```bash
source ~/vllm-env/bin/activate && VLLM_USE_FLASHINFER_SAMPLER=0 vllm serve Qwen/Qwen2.5-0.5B-Instruct --dtype bfloat16 --port 8001 --max-model-len 2048 --max-num-seqs 128 --gpu-memory-utilization 0.7
```

Ready when it prints `Uvicorn running on http://0.0.0.0:8001`; WSL2 forwards the port so it is reachable from Windows as `localhost:8001`. Flag notes:

- `--gpu-memory-utilization 0.7`: vLLM pre-allocates this fraction of *total* VRAM for weights plus KV cache. On a 6 GB laptop GPU the default 0.9 fails at startup because Windows already holds some VRAM.
- `--max-num-seqs`: vLLM's own batch cap. Keep it at or above the Go `-max-active` so the Go limit is the one that binds.
- `VLLM_USE_FLASHINFER_SAMPLER=0`: avoids a just-in-time kernel compile that needs the CUDA toolkit (`nvcc`), which a WSL install does not have. Costs nothing at temperature 0.

### 2. Start the Go service

Against the Python worker:

```bash
go run ./cmd/server -worker-url http://localhost:8000 -max-active 1
```

Against vLLM:

```bash
go run ./cmd/server -worker-url http://localhost:8001 -worker-kind vllm -max-active 64
```

Without `-worker-url`, the service uses a built-in fake worker that emits a fixed token sequence, which is useful for working on the Go side without a model. When running behind Nginx, add `-addr 127.0.0.1:8080` so the service is reachable only through the proxy.

### 3. Send a request

Streaming (SSE):

```bash
curl -N -X POST http://localhost:8080/v1/inference -H "Content-Type: application/json" -d '{"model":"qwen","prompt":"What is a goroutine?","max_output_tokens":64,"stream":true}'
```

```
event: token
data: {"text":"A"}

event: token
data: {"text":" goroutine"}
...
event: done
data: {"finish_reason":"stop","generated_tokens":42}
```

Non-streaming, one JSON response when generation finishes:

```bash
curl -X POST http://localhost:8080/v1/inference -H "Content-Type: application/json" -d '{"model":"qwen","prompt":"What is a goroutine?","max_output_tokens":64}'
```

On PowerShell, `curl.exe` and JSON quoting do not mix well; put the body in a file and pass `-d "@body.json"`.

## Running behind Nginx

`deploy/nginx.conf` puts Nginx in front of the Go service as the edge: TLS on 443, plain HTTP on 80, per-client rate limiting and the 1 MB body limit enforced before a request reaches Go, and an access log that carries the Go service's `X-Request-ID` so edge and service logs can be matched. Streaming is configured to pass each SSE event through as it is produced (`proxy_buffering off`), and the upstream read timeout is set above the Go `-total-timeout` so Nginx never cuts a long stream. `/health` is exempt from the rate limit; rejected requests get the same JSON error bodies the Go service uses (`429 too_many_requests` with `Retry-After: 1`, `413 request_too_large`).

Generate a self-signed certificate once (Git for Windows ships `openssl`; the path below is where it lives):

```bash
New-Item -ItemType Directory -Force deploy\certs; & "C:\Program Files\Git\usr\bin\openssl.exe" req -x509 -newkey rsa:2048 -nodes -keyout deploy\certs\key.pem -out deploy\certs\cert.pem -days 365 -subj "/CN=localhost"
```

`deploy/certs/` is git-ignored. Edit the two `ssl_certificate*` paths in `deploy/nginx.conf` to match your checkout, then start Nginx with its own folder as the prefix (for logs and temp files) and the repo config:

```bash
C:\nginx\nginx-1.30.5\nginx.exe -p C:\nginx\nginx-1.30.5 -c <repo>\deploy\nginx.conf
```

Test a config before reloading, reload, and stop with `-t -c <conf>`, `-s reload` and `-s stop` (all with the same `-p`). Then:

```bash
curl -k -i https://localhost/health
```

`-k` accepts the self-signed certificate. The per-IP rate limit (20 requests/s, burst 40) will throttle the load generator, which runs from one IP; raise it or comment out the `limit_req` line in the `/v1/inference` location for load tests.

## API summary

| Endpoint | Purpose |
|---|---|
| `POST /v1/inference` | Run inference. Body fields: `model`, `prompt` (required); `max_output_tokens` (default 256), `temperature` (default 0, greedy), `stream` (default false) |
| `GET /health` | 200 when the Go service is running |

Every response carries an `X-Request-ID` header; one is generated if the request has none.

Errors before streaming starts are JSON with an HTTP status: `400 invalid_request`, `413 request_too_large`, `429 too_many_requests` (from the edge proxy), `503 capacity_exceeded` (with `Retry-After: 1`), `503 worker_unavailable`, `504 inference_timeout`, `500 internal_error`. Once a stream has started the status can no longer change, so failures arrive as an `event: error` with the same `code`/`message` body and the connection closes. See design doc §4–§6 for the full rules.

## Configuration

All limits are flags on `cmd/server`. Durations use Go syntax (`30s`, `2m`). The service refuses to start if any value is zero or negative.

| Flag | Default | Meaning |
|---|---|---|
| `-addr` | `:8080` | Listen address; use `127.0.0.1:8080` behind Nginx |
| `-worker-url` | empty | Worker base URL; empty selects the fake worker |
| `-worker-kind` | `http` | `http` for the Python worker's protocol, `vllm` for the OpenAI-compatible protocol |
| `-worker-model` | `Qwen/Qwen2.5-0.5B-Instruct` | Model name sent to a vLLM worker (must match what vLLM serves) |
| `-max-active` | `2` | Concurrent inference requests. With the Python worker it must equal `MAX_CONCURRENT` (and 1 is the measured optimum); with vLLM it is the only admission control and 64 is a reasonable default |
| `-max-output-tokens` | `1024` | Upper bound a request may ask for |
| `-request-body-limit` | `1048576` | Request body limit in bytes |
| `-total-timeout` | `2m` | Maximum total inference time |
| `-first-token-timeout` | `30s` | Time allowed before the first token |
| `-idle-timeout` | `15s` | Maximum gap between tokens |
| `-shutdown-timeout` | `30s` | Grace period for active requests on shutdown |

Requests beyond `-max-active` are not queued; they get `503 capacity_exceeded` immediately.

## Worker protocols

**Version 1 (`-worker-kind http`).** The Go service is the worker's only client. `POST /generate` takes `{"prompt", "max_output_tokens", "temperature"}` and answers `200` with `application/x-ndjson`: one `{"text": ...}` line per chunk of text, then exactly one `{"done": true, "finish_reason": "stop"|"length", "generated_tokens": N}` line. If generation fails mid-stream the last line is `{"error": "..."}` instead. Any non-200 status (including the worker's own `503` when it is at capacity) means generation could not start. When the Go service closes the connection (client disconnect, timeout or shutdown), the worker stops generating and frees its slot. Details in design doc §9.

**Version 2 (`-worker-kind vllm`).** The Go service sends `POST /v1/chat/completions` with the same fixed system prompt the Python worker uses, `stream: true` and `stream_options.include_usage`, and parses vLLM's SSE stream (`data:` lines, `[DONE]` terminator) into the same token stream the handlers consume. vLLM queues rather than rejects when busy, so `-max-active` is the only capacity limit. Details in design doc §11.

## Tests

```bash
go test ./...
```

The Go tests use the fake worker and do not need Python, vLLM or a model.

## Benchmarking

`cmd/loadgen` sends streaming requests through the Go service and reports time-to-first-token, per-stream and aggregate tokens/s, latency percentiles, rejections and how many requests filled their token budget. Method: fixed prompt, 128 output tokens, temperature 0, one warm-up request, 50 requests per level (at least four waves for high concurrency), GPU utilization sampled alongside. Details in design doc §10.

```bash
go build -o loadgen.exe ./cmd/loadgen
```

```bash
.\loadgen.exe -concurrency 16 -requests 64 -warmup 1 -csv bench\v2\c16.csv
```

A row is valid only when the summary shows `rejected=0 failed=0` and `length=N/N` (every request generated the full 128 tokens). `-csv` writes one line per request so latency can be examined against start time. Sample the GPU in another terminal, writing UTF-8 so the file is parseable:

```bash
nvidia-smi --query-gpu=utilization.gpu,clocks.sm,clocks.max.sm,temperature.gpu --format=csv -l 1 | Out-File -Encoding utf8 bench\v2\gpu_c16.csv
```

### Benchmark results

RTX 4050 Laptop GPU (6 GB), Qwen2.5-0.5B-Instruct in bf16, 128 output tokens per request. Full tables, method and caveats in design doc §10–§11.

| Concurrency | v1 aggregate tok/s | v1 latency p50 | vLLM aggregate tok/s | vLLM latency p50 | vLLM per-stream tok/s |
|---|---|---|---|---|---|
| 1 | 40 | 2.85 s | 129 | 0.98 s | 131 |
| 4 | 49 | 10.2 s | 483 | 1.00 s | 132 |
| 16 | – | – | 1,413 | 1.06 s | 125 |
| 64 | – | – | 5,487 | 1.48 s | 92 |
| 128 | – | – | 8,305 | 1.96 s | 68 |

Two findings:

- **The v1 worker is CPU-bound, not GPU-bound.** Throughput stays flat at ~45 tok/s from 1 to 4 concurrent requests while latency grows linearly, and GPU utilization sits at ~43% throughout. Each request runs its own Python token loop, and the loops contend for the interpreter; more threads only divide the same work. The right `MAX_CONCURRENT` for it is 1.
- **vLLM removes both halves of that limit.** A single stream is 2.9× faster (CUDA graphs replace ~300 kernel launches per token with one), and one batched loop serves all requests, so throughput scales almost linearly to 32 concurrent requests at nearly constant per-user speed, then with diminishing returns as the GPU's compute becomes the limit. At 64 concurrent requests it delivers 40× the v1 worker's ceiling while keeping p50 latency under 1.5 s.
