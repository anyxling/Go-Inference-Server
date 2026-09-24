import json
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from transformers import AutoModelForCausalLM, AutoTokenizer, TextIteratorStreamer
import threading

PORT = 8000

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
            streamer = TextIteratorStreamer(tokenizer, skip_prompt=True, skip_special_tokens=True)
            generate_kwargs = dict(
                model_inputs,
                streamer=streamer,
                max_new_tokens=req.get("max_output_tokens", 256),
            )

            results = {}
            def _generate():
                results["ids"] = model.generate(**generate_kwargs)

            t = threading.Thread(target=_generate)
            t.start()

            self.send_response(200)
            self.send_header("Content-Type", "application/x-ndjson")
            self.end_headers()

            
            for chunk in streamer:
                self.wfile.write((json.dumps({"text": chunk}) + "\n").encode())
                self.wfile.flush()

            t.join()

            last_token = result["ids"][0][-1].item()
            finish_reason = "stop" if last_token in EOS_IDS else "length"
            
            self.wfile.write((json.dumps({"done": True, "finish_reason": finish_reason}) + "\n").encode())
            self.wfile.flush()
        except (BrokenPipeError, ConnectionResetError):
            return

if __name__ == "__main__":
    try:
        server = ThreadingHTTPServer(("", PORT), Handler)
        print(f"Listening on {PORT}")
        server.serve_forever()
    except (KeyboardInterrupt):
        server.server_close()
    
