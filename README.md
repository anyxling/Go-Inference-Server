# Go Inference Server

A small text-inference service: a Go HTTP server that validates requests, enforces limits and timeouts, and streams generated tokens to clients as Server-Sent Events, backed by a Python worker that runs [Qwen2.5-0.5B-Instruct](https://huggingface.co/Qwen/Qwen2.5-0.5B-Instruct) with Hugging Face `transformers`.

This is a learning project. Version 1 runs on one machine with one worker and one GPU, and deliberately does no batching so that a batching engine (vLLM) can be measured against it later. The full design, including the API contract, error codes, timeouts and the worker protocol, is in [design_doc.md](design_doc.md).

```
Client  --HTTP POST /v1/inference-->  Go service  --HTTP POST /generate-->  Python worker  -->  model
Client  <--SSE events---------------  Go service  <--NDJSON lines---------  Python worker
```

The Go service owns the public API, validation, request IDs, admission control, timeouts, cancellation and SSE streaming. The worker owns tokenization, model execution and the concurrency limit on the GPU. The two are separate processes that talk over plain HTTP.

## Repository layout

| Path | What it is |
|---|---|
| `cmd/server` | The Go inference service (`main.go` wires flags, the worker client and graceful shutdown) |
| `cmd/loadgen` | Load generator for the baseline benchmark in design doc §10 |
| `internal/api` | HTTP handlers, SSE writer, request-ID middleware, config and timeouts |
| `internal/worker` | The `Generator` interface, the HTTP worker client, and a fake worker for tests |
| `worker/server.py` | The Python inference worker |
| `requirements.txt` | Pinned Python dependencies (CUDA 12.6 build of PyTorch) |
| `design_doc.md` | Design and protocol reference |

## Prerequisites

- Go 1.26 or newer
- Python 3.10 or newer
- An NVIDIA GPU is recommended but not required; the worker falls back to CPU automatically (slower, but everything works)

## Setup

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

## Running

### 1. Start the worker

```bash
python worker/server.py
```

It loads the model, then listens on port 8000. Until loading finishes, connections are refused and the Go service reports `503 worker_unavailable`.

The number of generations the worker runs at once is set with an environment variable and defaults to 2. It must match the Go service's `-max-active`:

```bash
# PowerShell
$env:MAX_CONCURRENT = "2"; python worker/server.py
```

### 2. Start the Go service

```bash
go run ./cmd/server -worker-url http://localhost:8000
```

Without `-worker-url`, the service uses a built-in fake worker that emits a fixed token sequence, which is useful for working on the Go side without a model.

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

## API summary

| Endpoint | Purpose |
|---|---|
| `POST /v1/inference` | Run inference. Body fields: `model`, `prompt` (required); `max_output_tokens` (default 256), `temperature` (default 0, greedy), `stream` (default false) |
| `GET /health` | 200 when the Go service is running |

Every response carries an `X-Request-ID` header; one is generated if the request has none.

Errors before streaming starts are JSON with an HTTP status: `400 invalid_request`, `413 request_too_large`, `503 capacity_exceeded` (with `Retry-After: 1`), `503 worker_unavailable`, `504 inference_timeout`, `500 internal_error`. Once a stream has started the status can no longer change, so failures arrive as an `event: error` with the same `code`/`message` body and the connection closes. See design doc §4–§6 for the full rules.

## Configuration

All limits are flags on `cmd/server`. Durations use Go syntax (`30s`, `2m`). The service refuses to start if any value is zero or negative.

| Flag | Default | Meaning |
|---|---|---|
| `-addr` | `:8080` | Listen address |
| `-worker-url` | empty | Worker base URL; empty selects the fake worker |
| `-max-active` | `2` | Concurrent inference requests; must equal the worker's `MAX_CONCURRENT` |
| `-max-output-tokens` | `1024` | Upper bound a request may ask for |
| `-request-body-limit` | `1048576` | Request body limit in bytes |
| `-total-timeout` | `2m` | Maximum total inference time |
| `-first-token-timeout` | `30s` | Time allowed before the first token |
| `-idle-timeout` | `15s` | Maximum gap between tokens |
| `-shutdown-timeout` | `30s` | Grace period for active requests on shutdown |

Requests beyond `-max-active` are not queued; they get `503 capacity_exceeded` immediately.

## Worker protocol

The Go service is the worker's only client. `POST /generate` takes `{"prompt", "max_output_tokens", "temperature"}` and answers `200` with `application/x-ndjson`: one `{"text": ...}` line per token, then exactly one `{"done": true, "finish_reason": "stop"|"length"}` line. If generation fails mid-stream the last line is `{"error": "..."}` instead. Any non-200 status (including the worker's own `503` when it is at capacity) means generation could not start.

When the Go service closes the connection (client disconnect, timeout or shutdown), the worker stops generating and frees its slot. Details are in design doc §9.

## Tests

```bash
go test ./...
```

The Go tests use the fake worker and do not need Python or a model.

## Benchmarking

`cmd/loadgen` sends streaming requests through the Go service and reports time-to-first-token, per-stream and aggregate tokens/s, latency percentiles and rejections. The baseline method (fixed prompt, 128 output tokens, temperature 0, one warm-up, concurrency 1/2/4/8/16 with both limits raised to match, 50 requests per level) is in design doc §10.

```bash
go build -o loadgen ./cmd/loadgen
```

```bash
./loadgen -concurrency 2 -requests 50 -prompt "Write a long essay about the history of computers." -max-tokens 128
```

Use a prompt that reliably fills the token budget so every request does the same amount of work, and start the worker and the Go service with matching limits for each level. Sample `nvidia-smi` in a separate terminal during each run.
