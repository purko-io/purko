import importlib
import socket
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

WEBAPP = str(Path(__file__).resolve().parent.parent / "webapp")


class SSEUpstream(BaseHTTPRequestHandler):
    def log_message(self, *a):
        pass

    def do_GET(self):
        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.end_headers()
        for i in range(3):
            self.wfile.write(f"data: {{\"n\": {i}}}\n\n".encode())
            self.wfile.flush()
            time.sleep(0.3)


def test_sse_frames_arrive_incrementally():
    up = ThreadingHTTPServer(("127.0.0.1", 0), SSEUpstream)
    threading.Thread(target=up.serve_forever, daemon=True).start()
    import serve
    importlib.reload(serve)
    srv = serve.make_server(WEBAPP, up.server_address[1])
    threading.Thread(target=srv.serve_forever, daemon=True).start()
    try:
        s = socket.create_connection(("127.0.0.1", srv.server_address[1]), timeout=5)
        s.sendall(b"GET /api/events HTTP/1.1\r\nHost: x\r\n\r\n")
        s.settimeout(5)
        start = time.monotonic()
        buf = b""
        while b'data: {"n": 0}' not in buf:
            buf += s.recv(4096)
        first_frame_at = time.monotonic() - start
        # frame 0 must arrive before the upstream finishes (~0.9s total)
        assert first_frame_at < 0.8, f"first frame too late: {first_frame_at:.2f}s"
        while b'data: {"n": 2}' not in buf:
            buf += s.recv(4096)
        s.close()
    finally:
        srv.shutdown(); up.shutdown()
