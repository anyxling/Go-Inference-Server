import json
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

TOKEN_DELAY = 0.1
PORT = 8000

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
            tokens = [" " + w for w in req["prompt"].split()]
            max_tokens = req.get("max_output_tokens", 256)
            if len(tokens) > max_tokens:
                tokens = tokens[:max_tokens]
                finish_reason = "length"
            else:
                finish_reason = "stop"
            self.send_response(200)
            self.send_header("Content-Type", "application/x-ndjson")
            self.end_headers()
            for tok in tokens:
                self.wfile.write((json.dumps({"text": tok}) + "\n").encode())
                self.wfile.flush()
                time.sleep(TOKEN_DELAY)
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
    
