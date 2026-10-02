import argparse
import functools
import http.server
import json
import threading
import time
from pathlib import Path
from urllib.parse import parse_qs, urlsplit

root = Path(__file__).resolve().parents[1]


class MediaServer(http.server.ThreadingHTTPServer):
    offline = False
    denied_requests = 0

    def __init__(self, address, handler):
        self.state_lock = threading.Lock()
        super().__init__(address, handler)


class MediaHandler(http.server.SimpleHTTPRequestHandler):
    def copyfile(self, source, outputfile):
        if parse_qs(urlsplit(self.path).query).get('slow') != ['1']:
            return super().copyfile(source, outputfile)
        try:
            while True:
                block = source.read(2048)
                if not block:
                    return
                outputfile.write(block)
                outputfile.flush()
                time.sleep(0.08)
        except (BrokenPipeError, ConnectionResetError):
            return

    def status(self):
        with self.server.state_lock:
            body = json.dumps({'offline': self.server.offline,
                               'deniedRequests': self.server.denied_requests}).encode()
        self.send_response(200)
        self.send_header('Content-Type', 'application/json')
        self.send_header('Content-Length', str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_POST(self):
        action = urlsplit(self.path).path
        if action not in ('/_test/offline', '/_test/online'):
            self.send_error(404)
            return
        with self.server.state_lock:
            self.server.offline = action == '/_test/offline'
            self.server.denied_requests = 0
        self.status()

    def do_GET(self):
        if urlsplit(self.path).path == '/_test/status':
            self.status()
            return
        with self.server.state_lock:
            offline = self.server.offline
            if offline:
                self.server.denied_requests += 1
        if offline:
            self.send_error(503, 'Synthetic media source is offline')
        else:
            super().do_GET()


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--directory', type=Path, default=root / 'build' / 'device-test' / 'media')
    parser.add_argument('--port', type=int, default=38473)
    options = parser.parse_args()
    handler = functools.partial(MediaHandler, directory=str(options.directory.resolve()))
    with MediaServer(('127.0.0.1', options.port), handler) as server:
        print('Synthetic media server: http://127.0.0.1:' + str(options.port), flush=True)
        try:
            server.serve_forever()
        except KeyboardInterrupt:
            pass


if __name__ == '__main__':
    main()
