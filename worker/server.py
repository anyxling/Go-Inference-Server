import json
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from transformers import AutoModelForCausalLM, AutoTokenizer, TextIteratorStreamer, StoppingCriteria, StoppingCriteriaList
import threading
import torch
import queue

PORT = 8000
MAX_CONCURRENT = 4

sem = threading.Semaphore(MAX_CONCURRENT)

model_name = "Qwen/Qwen2.5-0.5B-Instruct"

model = AutoModelForCausalLM.from_pretrained(
    model_name,
    torch_dtype="auto",
    device_map="auto"
)
tokenizer = AutoTokenizer.from_pretrained(model_name)

EOS_IDS = model.generation_config.eos_token_id
if isinstance(EOS_IDS, int):
    EOS_IDS = [EOS_IDS]


class StopOnEvent(StoppingCriteria):
    def __init__(self, stop_event):
        self.stop = stop_event

    def __call__(self, input_ids, scores, **kwargs):
        # called after each new token
        # return True → stop generating
        done = self.stop.is_set()
        return torch.full((input_ids.shape[0],), done, dtype=torch.bool, device=input_ids.device)


class Handler(BaseHTTPRequestHandler):

    def do_GET(self):
        if self.path == "/health":
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(json.dumps({"status": "ok"}).encode())
        else:
            self.send_error(404)

    def do_POST(self):
        if self.path != "/generate":
            self.send_error(404)
            return
        length = int(self.headers.get("Content-Length", 0))
        raw = self.rfile.read(length)

        if not sem.acquire(blocking=False):
                self.send_error(503)
                return
                
        try:
            try:
                req = json.loads(raw)
            except (json.JSONDecodeError):
                self.send_error(400)
                return
            if not req.get("prompt"):
                self.send_error(400)
                return

            messages = [
                {"role": "system", "content": "You are Qwen, created by Alibaba Cloud. You are a helpful assistant."},
                {"role": "user", "content": req.get("prompt")}
            ]
            text = tokenizer.apply_chat_template(
                messages,
                tokenize=False,
                add_generation_prompt=True
            )
            model_inputs = tokenizer([text], return_tensors="pt").to(model.device)
            streamer = TextIteratorStreamer(tokenizer, skip_prompt=True, skip_special_tokens=True, timeout=10)
            results = {}
            stop = threading.Event()
            generate_kwargs = dict(
                model_inputs,
                streamer=streamer,
                max_new_tokens=req.get("max_output_tokens", 256),
                stopping_criteria=StoppingCriteriaList([StopOnEvent(stop)]),
            )

            def _generate():
                try:
                    results["ids"] = model.generate(**generate_kwargs)
                except Exception as e:
                    results["error"] = e
                    streamer.end()

            t = threading.Thread(target=_generate)
            t.start()

            self.send_response(200)
            self.send_header("Content-Type", "application/x-ndjson")
            self.end_headers()

            try:
                for chunk in streamer:
                    if chunk != "":
                        self.wfile.write((json.dumps({"text": chunk}) + "\n").encode())
                        self.wfile.flush()
            except queue.Empty:
                results.setdefault("error", "generation timed out")
            except (ConnectionError):
                stop.set()
                return
            finally:
                stop.set()
                t.join()
                sem.release()
            
            if "error" in results:
                self.wfile.write((json.dumps({"error": str(results["error"])}) + "\n").encode())
                self.wfile.flush()
                return

            last_token = results["ids"][0][-1].item()
            finish_reason = "stop" if last_token in EOS_IDS else "length"
            
            self.wfile.write((json.dumps({"done": True, "finish_reason": finish_reason}) + "\n").encode())
            self.wfile.flush()
        except (ConnectionError): 
            return
        

if __name__ == "__main__":
    try:
        server = ThreadingHTTPServer(("", PORT), Handler)
        print(f"Listening on {PORT}")
        server.serve_forever()
    except (KeyboardInterrupt):
        server.server_close()
    
