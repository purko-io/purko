"""Local Mission Control server: static webapp + /api reverse proxy."""
import argparse
import atexit
import functools
import http.client
import http.server
import json
import signal
import sys
import threading
import urllib.parse


def _parse_upstream_url(url):
    """Validate and parse a remote upstream URL.

    Returns (scheme, host, port, base_path).
    Raises ValueError for unsupported schemes or unparseable URLs.
    """
    p = urllib.parse.urlparse(url)
    if p.scheme not in ("http", "https"):
        raise ValueError(
            f"upstream URL scheme must be http or https, got {p.scheme!r}")
    host = p.hostname
    port = p.port or (443 if p.scheme == "https" else 80)
    base = p.path.rstrip("/")
    return p.scheme, host, port, base


class ProxyState:
    def __init__(self, upstream_port=None, upstream_url=None):
        self.upstream_port = upstream_port  # kubectl port-forward local port
        self.upstream_url = upstream_url    # remote URL (mutually exclusive with port)
        self.token = None                   # dashboard bearer token (memory only)
        self.auth_mode = None               # "open" | "token" | "sso" | None
        # Pre-parsed URL fields — populated by make_server when upstream_url is set.
        self._url_scheme = None
        self._url_host = None
        self._url_port = None
        self._url_base = ""


class Handler(http.server.SimpleHTTPRequestHandler):
    # state injected via functools.partial in make_server
    def __init__(self, *args, state=None, **kwargs):
        self.state = state
        super().__init__(*args, **kwargs)

    def log_message(self, fmt, *args):  # keep test output clean
        pass

    def _json(self, code, obj):
        body = json.dumps(obj).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def _local_status(self):
        self._json(200, {
            "upstream": (self.state.upstream_port is not None
                         or self.state.upstream_url is not None),
            "authMode": self.state.auth_mode,
            "loggedIn": self.state.token is not None,
            # Where the user manages their own namespace (Spec 40 dashboard,
            # scoped per-user in sso mode). Hosted: the workspace URL serves
            # the operator's dashboard directly. Cluster: the operator
            # dashboard is reachable at the local port-forward.
            "dashboardUrl": self._dashboard_url(),
        })

    def _dashboard_url(self):
        """The full authoring/management dashboard URL, or None if unknown."""
        if self.state.upstream_url:
            scheme = self.state._url_scheme or "https"
            host = self.state._url_host
            port = self.state._url_port
            if not host:
                return self.state.upstream_url
            default = 443 if scheme == "https" else 80
            netloc = host if (port is None or port == default) else f"{host}:{port}"
            return f"{scheme}://{netloc}{self.state._url_base}"
        if self.state.upstream_port:
            # Cluster mode: the operator serves its dashboard (UI + API) on the
            # forwarded port. Token mode shows its own login there.
            return f"http://127.0.0.1:{self.state.upstream_port}"
        return None

    HOP_HEADERS = {
        "connection", "keep-alive", "proxy-authenticate", "proxy-authorization",
        "te", "trailers", "transfer-encoding", "upgrade", "host",
    }

    def _origin_allowed(self):
        """Return False if Origin (or Referer) shows a cross-origin mutating request."""
        origin = self.headers.get("Origin")
        if not origin:
            referer = self.headers.get("Referer", "")
            if referer:
                p = urllib.parse.urlparse(referer)
                if p.scheme and p.netloc:
                    origin = f"{p.scheme}://{p.netloc}"
        if not origin:
            return True  # no Origin/Referer — curl, skill itself, etc. — allow
        port = self.server.server_address[1]
        allowed = {f"http://127.0.0.1:{port}", f"http://localhost:{port}"}
        return origin in allowed

    # ------------------------------------------------------------------
    # Connection helpers — shared by _proxy and _local_token
    # ------------------------------------------------------------------

    def _open_conn(self, timeout=310):
        """Open a connection to the upstream (port-forward or remote URL)."""
        s = self.state
        if s.upstream_port is not None:
            return http.client.HTTPConnection("127.0.0.1", s.upstream_port, timeout=timeout)
        cls = (http.client.HTTPSConnection
               if s._url_scheme == "https"
               else http.client.HTTPConnection)
        return cls(s._url_host, s._url_port, timeout=timeout)

    def _remote_path(self, path):
        """Prepend the URL base path in remote-URL mode; unchanged for port mode."""
        if self.state.upstream_url is not None:
            return self.state._url_base + path
        return path

    def _host_header(self):
        """Return the Host header value for URL mode; None for port mode.

        Port mode strips 'host' as a hop-by-hop header (existing behaviour).
        URL mode must re-set Host to the remote host so ingress routing works.
        """
        s = self.state
        if s.upstream_url is None:
            return None
        default_port = 443 if s._url_scheme == "https" else 80
        if s._url_port == default_port:
            return s._url_host
        return f"{s._url_host}:{s._url_port}"

    # ------------------------------------------------------------------

    def _proxy(self):
        if self.state.upstream_port is None and self.state.upstream_url is None:
            return self._json(503, {"error": "no cluster connected"})
        if self.command in ("POST", "PUT", "DELETE", "PATCH") and not self._origin_allowed():
            return self._json(403, {"error": "cross-origin request rejected"})
        length = int(self.headers.get("Content-Length", 0) or 0)
        body = self.rfile.read(length) if length else None
        headers = {k: v for k, v in self.headers.items()
                   if k.lower() not in self.HOP_HEADERS}
        if self.state.token:
            headers["Authorization"] = f"Bearer {self.state.token}"
            headers["X-Purko-CSRF"] = "1"
        host_hdr = self._host_header()
        if host_hdr is not None:
            headers["Host"] = host_hdr
        conn = resp = None
        for attempt in range(2):
            try:
                conn = self._open_conn(timeout=310)
                conn.request(self.command, self._remote_path(self.path),
                             body=body, headers=headers)
                resp = conn.getresponse()
                break
            except (OSError, http.client.HTTPException):
                if conn is not None:
                    conn.close()
                    conn = None
                pf = getattr(self.server, "portforward", None)
                if pf is not None and attempt == 0:
                    with self.server._pf_lock:
                        try:
                            pf.ensure()
                            self.state.upstream_port = pf.local_port
                        except Exception:
                            return self._json(502, {"error": "upstream unreachable"})
                    continue
                return self._json(502, {"error": "upstream unreachable"})
        try:
            self.send_response(resp.status)
            for k, v in resp.getheaders():
                if k.lower() not in self.HOP_HEADERS:
                    self.send_header(k, v)
            self.end_headers()
            while True:
                chunk = resp.read1(8192)
                if not chunk:
                    break
                self.wfile.write(chunk)
                self.wfile.flush()
        except (BrokenPipeError, ConnectionResetError, OSError):
            pass  # browser closed (e.g. SSE tab gone) or idle socket timeout — normal
        finally:
            conn.close()

    def do_GET(self):
        if self.path == "/local/status":
            return self._local_status()
        if self.path == "/local/version":
            return self._local_version()
        if self.path.startswith("/api/"):
            return self._proxy()
        return super().do_GET()

    def _local_version(self):
        """Skill self-version status for the Mission Control update chip.
        Best-effort and silent-fail — a version check never breaks the page."""
        try:
            import version_check
            return self._json(200, version_check.check())
        except Exception:
            return self._json(200, {"local": "", "latest": None, "update_available": False})

    def _local_token(self):
        if not self._origin_allowed():
            return self._json(403, {"error": "cross-origin request rejected"})
        length = int(self.headers.get("Content-Length", 0) or 0)
        try:
            payload = json.loads(self.rfile.read(length))
            token = payload["token"]
        except (json.JSONDecodeError, KeyError, TypeError):
            return self._json(400, {"error": "expected JSON {\"token\": ...}"})
        if self.state.upstream_port is None and self.state.upstream_url is None:
            return self._json(503, {"error": "no cluster connected"})
        whoami_path = self._remote_path("/api/whoami")
        req_headers = {"Authorization": f"Bearer {token}"}
        host_hdr = self._host_header()
        if host_hdr is not None:
            req_headers["Host"] = host_hdr
        conn = resp = body = None
        for attempt in range(2):
            conn = self._open_conn(timeout=10)
            try:
                conn.request("GET", whoami_path, headers=req_headers)
                resp = conn.getresponse()
                body = resp.read()
                break
            except (OSError, http.client.HTTPException):
                conn.close()
                conn = None
                pf = getattr(self.server, "portforward", None)
                if pf is not None and attempt == 0:
                    with self.server._pf_lock:
                        try:
                            pf.ensure()
                            self.state.upstream_port = pf.local_port
                        except Exception:
                            return self._json(502, {"error": "upstream unreachable"})
                    continue
                return self._json(502, {"error": "upstream unreachable"})
            finally:
                if conn is not None:
                    conn.close()
                    conn = None
        if resp.status in (401, 403):
            return self._json(401, {"error": "invalid token"})
        if resp.status != 200:
            return self._json(502, {"error": "upstream error"})
        try:
            self.state.auth_mode = json.loads(body).get("mode")
        except json.JSONDecodeError:
            pass
        self.state.token = token
        self.send_response(204)
        self.end_headers()

    def _local_token_remember(self):
        """Persist the in-memory token to the OS keychain — no token on argv or history."""
        if not self._origin_allowed():
            return self._json(403, {"error": "cross-origin request rejected"})
        if self.state.token is None:
            return self._json(409, {"error": "not logged in"})
        length = int(self.headers.get("Content-Length", 0) or 0)
        try:
            payload = json.loads(self.rfile.read(length))
            target = str(payload["target"])
        except (json.JSONDecodeError, KeyError, TypeError, ValueError):
            return self._json(400, {"error": 'expected JSON {"target": ...}'})
        import keychain  # lazy: only loaded when the user opts to persist a token
        try:
            keychain.save_token(target, self.state.token)
        except ValueError:
            return self._json(400, {"error": "invalid target name"})
        self.send_response(204)
        self.end_headers()

    def do_POST(self):
        if self.path == "/local/token":
            return self._local_token()
        if self.path == "/local/token/remember":
            return self._local_token_remember()
        if self.path.startswith("/api/"):
            return self._proxy()
        self.send_error(405)

    def do_PUT(self):
        if self.path.startswith("/api/"):
            return self._proxy()
        self.send_error(405)

    def do_DELETE(self):
        if self.path.startswith("/api/"):
            return self._proxy()
        self.send_error(405)


def make_server(webapp_dir, upstream_port=None, host="127.0.0.1", port=0,
                upstream_url=None, portforward=None):
    if upstream_port is not None and upstream_url is not None:
        raise ValueError("upstream_port and upstream_url are mutually exclusive")
    state = ProxyState(upstream_port=upstream_port, upstream_url=upstream_url)
    if upstream_url is not None:
        scheme, url_host, url_port, url_base = _parse_upstream_url(upstream_url)
        state._url_scheme = scheme
        state._url_host = url_host
        state._url_port = url_port
        state._url_base = url_base
        if scheme == "http":
            print(f"WARNING: insecure http upstream {upstream_url!r}",
                  file=sys.stderr, flush=True)
    handler = functools.partial(Handler, directory=webapp_dir, state=state)
    srv = http.server.ThreadingHTTPServer((host, port), handler)
    srv.state = state
    srv.portforward = portforward
    srv._pf_lock = threading.Lock()
    if portforward is not None:
        srv._shutdown_pf = portforward.stop
        srv._orig_shutdown = srv.shutdown

        def _shutdown_with_pf():
            srv._orig_shutdown()
            srv._shutdown_pf()

        srv.shutdown = _shutdown_with_pf
    return srv


def _install_signal_handlers():
    """Install OS signal handlers so SIGTERM unwinds through finally blocks."""
    signal.signal(signal.SIGTERM, lambda *_: sys.exit(0))


def main(argv=None):
    ap = argparse.ArgumentParser()
    ap.add_argument("--webapp", default="webapp")
    up = ap.add_mutually_exclusive_group()
    up.add_argument("--upstream-port", type=int, default=None)
    up.add_argument("--upstream-url", default=None)
    up.add_argument("--pf-context", default=None, metavar="CTX",
                    help="kubectl context; serve.py owns the port-forward lifecycle")
    ap.add_argument("--pf-namespace", default="purko-system",
                    help="namespace for port-forward (default: purko-system)")
    ap.add_argument("--pf-target", default="deploy/purko-operator",
                    help="port-forward target (default: deploy/purko-operator)")
    ap.add_argument("--pf-remote-port", type=int, default=8082,
                    help="remote port for port-forward (default: 8082)")
    ap.add_argument("--port", type=int, default=8090,
                    help="stable default 8090; falls back to a random port if taken")
    ap.add_argument("--token-stdin", action="store_true",
                    help="read a dashboard bearer token from stdin")
    args = ap.parse_args(argv)

    pf = None
    upstream_port = args.upstream_port
    if args.pf_context:
        import cluster
        pf = cluster.PortForward(
            args.pf_context,
            namespace=args.pf_namespace,
            target=args.pf_target,
            remote_port=args.pf_remote_port,
        )
        try:
            pf.start(timeout=15)
        except RuntimeError as exc:
            print(f"error: port-forward failed: {exc}", file=sys.stderr, flush=True)
            sys.exit(1)
        upstream_port = pf.local_port
        atexit.register(pf.stop)

    try:
        try:
            srv = make_server(args.webapp, upstream_port=upstream_port,
                              port=args.port, upstream_url=args.upstream_url,
                              portforward=pf)
        except OSError:
            srv = make_server(args.webapp, upstream_port=upstream_port,
                              port=0, upstream_url=args.upstream_url,
                              portforward=pf)
    except ValueError as e:
        ap.error(str(e))
    if args.token_stdin:
        srv.state.token = sys.stdin.readline().strip() or None
    print(f"http://127.0.0.1:{srv.server_address[1]}", flush=True)
    _install_signal_handlers()
    try:
        srv.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        if pf is not None:
            pf.stop()


if __name__ == "__main__":
    main()
