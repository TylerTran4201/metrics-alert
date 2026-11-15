import json
from http.server import BaseHTTPRequestHandler, HTTPServer


class Webhook(BaseHTTPRequestHandler):
    def do_POST(self):
        if self.path != "/alerts":
            self.send_error(404)
            return
        payload = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
        print(payload["text"])
        self.send_response(200)
        self.end_headers()
        self.wfile.write(b"ok")


HTTPServer(("0.0.0.0", 8080), Webhook).serve_forever()
