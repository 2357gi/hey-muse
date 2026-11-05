"""hey-muse speech adapter for the existing local VOICEVOX Engine. Stdlib only."""
import argparse
import hashlib
import hmac
import io
import ipaddress
import json
import logging
import os
import re
import socket
import threading
import subprocess
import sys
from pathlib import Path
import urllib.error
import urllib.parse
import urllib.request
import wave
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

LOG = logging.getLogger('speech-bridge')
MAX_BODY = 32768
MAX_TEXT = 1000
MAX_AUDIO = 16 * 1024 * 1024
RATE = 24000
VOICES = {'alloy': 3, 'zundamon': 3}

CLOCK_TIME = re.compile(
    r'(?<![\dA-Za-z_/:：.．])([0-9０-９]{1,2})[:：]([0-9０-９]{2})(?![\d:：/]|[.．]\d)'
)


def spoken_clock_time(match):
    hour, minute = map(int, match.groups())
    if minute >= 60 or hour > 24 or (hour == 24 and minute != 0):
        return match.group()
    return f'{hour}時' + (f'{minute}分' if minute else '')


def speech_parts(text):
    """Keep sentence punctuation and closing quotes with the words they end."""
    parts = []
    start = 0
    for match in re.finditer(r'(?:[。！？!?]+|\.(?=\s|$))[」』）】”’"]*|\n+', text):
        parts.append(text[start:match.end()])
        start = match.end()
    parts.append(text[start:])
    result = []
    for part in parts:
        # Long Japanese sentences often have a natural pause before the full stop.
        clauses = re.split(r'(?<=[、，])|\u3000+', part) if len(part) > 36 else [part]
        for clause in clauses:
            if result and clause.strip() and re.fullmatch(r'[。！？!?,、，.」』）】”’"]+', clause.strip()):
                result[-1] += clause.strip()
            elif clause.strip():
                result.append(clause.strip())
    return result or [text.strip()]


class SleepGuard:
    """Thread-scoped Windows idle-sleep request; never changes the power plan."""
    CONTINUOUS = 0x80000000
    SYSTEM_REQUIRED = 0x00000001

    def __init__(self, enabled=False, set_state=None):
        self.enabled = enabled
        self.active = False
        self.set_state = set_state
        self.previous = None

    def __enter__(self):
        if not self.enabled:
            return self
        if self.set_state is None:
            if os.name != 'nt':
                raise RuntimeError('Idle-sleep prevention requires Windows')
            import ctypes
            self.set_state = ctypes.WinDLL('kernel32', use_last_error=True).SetThreadExecutionState
            self.set_state.argtypes = [ctypes.c_uint]
            self.set_state.restype = ctypes.c_uint
        self.owner = threading.get_ident()
        flags = self.CONTINUOUS | self.SYSTEM_REQUIRED
        self.previous = self.set_state(flags)
        if not self.previous:
            raise OSError('Windows refused the idle-sleep request')
        confirmed_previous = self.set_state(flags)
        if not confirmed_previous or not confirmed_previous & self.SYSTEM_REQUIRED:
            self.set_state(self.previous | self.CONTINUOUS)
            raise OSError('Windows did not retain the idle-sleep request')
        self.active = True
        return self

    def __exit__(self, *_exc):
        if self.active:
            if threading.get_ident() != self.owner:
                raise RuntimeError('Idle-sleep request must be cleared on its owning thread')
            try:
                if not self.set_state(self.previous | self.CONTINUOUS):
                    raise OSError('Windows refused to restore the thread execution state')
            finally:
                self.active = False


class InvalidRequest(Exception):
    pass


class BackendUnavailable(Exception):
    pass


def pcm_from_wav(data):
    try:
        with wave.open(io.BytesIO(data), 'rb') as wav:
            if (wav.getnchannels(), wav.getsampwidth(), wav.getframerate(), wav.getcomptype()) != (1, 2, RATE, 'NONE'):
                raise BackendUnavailable('VOICEVOX returned an unexpected audio format')
            frames = wav.getnframes()
            pcm = wav.readframes(frames)
            if len(pcm) != frames * 2 or not pcm or len(pcm) % 2:
                raise BackendUnavailable('VOICEVOX returned incomplete audio')
            return pcm
    except (wave.Error, EOFError) as exc:
        raise BackendUnavailable('VOICEVOX returned invalid WAV') from exc


class Voicevox:
    def __init__(self, port=50021):
        self.base = f'http://127.0.0.1:{port}'
        self.http = urllib.request.build_opener(urllib.request.ProxyHandler({}))
        self.lock = threading.Lock()

    def request(self, path, body=None, limit=MAX_AUDIO):
        req = urllib.request.Request(self.base + path, data=body, method='GET' if body is None else 'POST')
        if body is not None:
            req.add_header('Content-Type', 'application/json')
        try:
            with self.http.open(req, timeout=60) as response:
                data = response.read(limit + 1)
                if len(data) > limit:
                    raise BackendUnavailable('VOICEVOX response exceeded the size limit')
                return data
        except (urllib.error.URLError, OSError, TimeoutError) as exc:
            raise BackendUnavailable('Local VOICEVOX Engine is unavailable') from exc

    def health(self):
        try:
            version = json.loads(self.request('/version', limit=1024))
            if not isinstance(version, str):
                return False
            return version == '0.26.0'
        except (BackendUnavailable, ValueError):
            return False

    def synthesize(self, text, speaker):
        if not self.lock.acquire(blocking=False):
            raise BackendUnavailable('VOICEVOX is busy; retry after the current utterance')
        try:
            query = urllib.parse.urlencode({'text': text, 'speaker': speaker})
            try:
                audio_query = json.loads(self.request('/audio_query?' + query, b'', limit=2 * 1024 * 1024))
                if not isinstance(audio_query, dict):
                    raise ValueError('not an object')
            except ValueError as exc:
                raise BackendUnavailable('VOICEVOX returned an invalid audio query') from exc
            audio_query['outputSamplingRate'] = RATE
            audio_query['outputStereo'] = False
            wav = self.request('/synthesis?' + urllib.parse.urlencode({'speaker': speaker}),
                               json.dumps(audio_query, ensure_ascii=False).encode('utf-8'))
            return pcm_from_wav(wav)
        finally:
            self.lock.release()


class Server(ThreadingHTTPServer):
    daemon_threads = True
    request_queue_size = 4

    def __init__(self, address, backend, allowed_network, sleep_guard=None, token=None):
        self.token = token or os.environ.get("SPEECH_BRIDGE_TOKEN", "")
        if not self.token:
            raise ValueError("SPEECH_BRIDGE_TOKEN is required")
        self.backend = backend
        self.sleep_guard = sleep_guard
        self.allowed_network = ipaddress.ip_network(allowed_network)
        self.slots = threading.BoundedSemaphore(4)
        self.speech_slot = threading.Lock()
        super().__init__(address, Handler)

    def process_request(self, request, client_address):
        if not self.slots.acquire(blocking=False):
            self.shutdown_request(request)
            return
        try:
            super().process_request(request, client_address)
        except BaseException:
            self.slots.release()
            raise

    def process_request_thread(self, request, client_address):
        try:
            super().process_request_thread(request, client_address)
        finally:
            self.slots.release()


class Handler(BaseHTTPRequestHandler):
    server_version = 'MuseSpeechBridge/1'
    sys_version = ''
    protocol_version = 'HTTP/1.1'

    def setup(self):
        super().setup()
        self.connection.settimeout(10)

    def log_message(self, *_args):
        # Never log input, URL query, authorization, or backend responses.
        pass

    def authorized_peer(self):
        peer = ipaddress.ip_address(self.client_address[0])
        if not (peer.is_loopback or peer in self.server.allowed_network):
            return False
        try:
            host = urllib.parse.urlsplit('http://' + self.headers.get('Host', '')).hostname
        except ValueError:
            return False
        return host in ('127.0.0.1', 'localhost', self.server.server_address[0])

    def respond(self, code, data, content_type='application/json'):
        self.close_connection = True
        try:
            self.send_response(code)
            self.send_header('Content-Type', content_type)
            self.send_header('Content-Length', str(len(data)))
            self.send_header('Connection', 'close')
            self.send_header('Cache-Control', 'no-store')
            self.send_header('X-Content-Type-Options', 'nosniff')
            self.end_headers()
            self.wfile.write(data)
        except (BrokenPipeError, ConnectionResetError, socket.timeout):
            pass

    def error(self, code, message):
        self.respond(code, json.dumps({'error': {'message': message, 'type': 'speech_bridge_error'}}).encode())

    def do_GET(self):
        if not self.authorized_peer():
            return self.error(403, 'Peer or Host is not allowed')
        if self.path != '/health':
            return self.error(404, 'Not found')
        ready = self.server.backend.health()
        self.respond(200 if ready else 503, json.dumps({'ready': ready, 'model': 'voicevox',
                      'speaker_id': 3, 'sample_rate': RATE, 'format': 'pcm_s16le_mono',
                      'idle_sleep_prevented': bool(self.server.sleep_guard and self.server.sleep_guard.active)}).encode())

    def do_POST(self):
        if not self.authorized_peer():
            return self.error(403, 'Peer or Host is not allowed')
        if self.path in ('/echo-home/v1', '/echo-hall/v1'):
            return self.echo_home()
        if self.path not in ('/v1/audio/speech', '/audio/speech'):
            return self.error(404, 'Not found')
        if not hmac.compare_digest(self.headers.get('Authorization', ''), 'Bearer ' + self.server.token):
            return self.error(401, 'Invalid speech token')
        if self.headers.get('Transfer-Encoding'):
            return self.error(400, 'Transfer-Encoding requests are unsupported')
        if self.headers.get_content_type() != 'application/json':
            return self.error(415, 'Content-Type must be application/json')
        lengths = self.headers.get_all('Content-Length', [])
        if len(lengths) != 1 or not lengths[0].isdigit():
            return self.error(411, 'One Content-Length is required')
        if len(lengths[0]) > 6:
            return self.error(413, 'Request body is too large')
        length = int(lengths[0])
        if length <= 0 or length > MAX_BODY:
            return self.error(413, 'Request body is too large or empty')
        try:
            raw = self.rfile.read(length)
            if len(raw) != length:
                raise InvalidRequest('Incomplete request body')
            body = json.loads(raw)
            if not isinstance(body, dict):
                raise InvalidRequest('JSON object required')
            if body.get('model') != 'voicevox':
                raise InvalidRequest('model must be voicevox')
            if body.get('response_format', 'pcm') != 'pcm':
                raise InvalidRequest('Only response_format=pcm is supported')
            if body.get('stream') or body.get('stream_format'):
                raise InvalidRequest('SSE streaming is unsupported')
            text = body.get('input')
            if not isinstance(text, str) or not text.strip() or len(text) > MAX_TEXT or '\0' in text:
                raise InvalidRequest(f'input must be 1 to {MAX_TEXT} characters')
            voice = body.get('voice', 'alloy')
            if not isinstance(voice, str) or voice not in VOICES:
                raise InvalidRequest('voice must be alloy or zundamon')
            style = body.get('instructions', '')
            if not isinstance(style, str) or len(style) > 500:
                raise InvalidRequest('instructions must be at most 500 characters')
        except (ValueError, UnicodeError, InvalidRequest) as exc:
            return self.error(400, str(exc) if isinstance(exc, InvalidRequest) else 'Invalid JSON')
        except (TimeoutError, socket.timeout):
            return self.error(408, 'Request timed out')
        except BackendUnavailable as exc:
            return self.error(503, str(exc))
        if not self.server.speech_slot.acquire(blocking=False):
            return self.error(503, 'VOICEVOX is busy; retry after the current utterance')
        try:
            self.send_speech(text, VOICES[voice])
        finally:
            self.server.speech_slot.release()

    def send_speech(self, text, speaker):
        parts = speech_parts(text) if self.request_version == 'HTTP/1.1' else [text]
        parts = [CLOCK_TIME.sub(spoken_clock_time, part) for part in parts]
        try:
            first = self.server.backend.synthesize(parts[0], speaker)
        except BackendUnavailable as exc:
            return self.error(503, str(exc))
        if len(parts) == 1:
            return self.respond(200, first, 'application/octet-stream')

        # HTTP chunk framing is stripped by the Echo's client. The terminating
        # chunk distinguishes a complete answer from a failure after speech began.
        self.close_connection = True
        try:
            self.send_response(200)
            self.send_header('Content-Type', 'application/octet-stream')
            self.send_header('Transfer-Encoding', 'chunked')
            self.send_header('Cache-Control', 'no-store')
            self.send_header('X-Content-Type-Options', 'nosniff')
            self.send_header('Connection', 'close')
            self.end_headers()
            self.write_chunk(first)
            for part in parts[1:]:
                self.write_chunk(self.server.backend.synthesize(part, speaker))
            self.wfile.write(b'0\r\n\r\n')
        except BackendUnavailable:
            LOG.warning('Synthesis interrupted before the final audio chunk')
        except (ConnectionError, socket.timeout):
            pass

    def echo_home(self):
        root = Path(__file__).resolve().parent
        lengths = self.headers.get_all('Content-Length', [])
        if self.headers.get('Transfer-Encoding') or len(lengths) != 1 or not re.fullmatch(r'[0-9]{1,4}', lengths[0]) or not 1 <= int(lengths[0]) <= 4096:
            return self.error(400, 'Invalid request')
        try:
            packet = self.rfile.read(int(lengths[0]))
            key = bytes.fromhex(json.loads((root/'home-local.json').read_text(encoding='utf-8'))['key'])
            signature = hmac.new(key, packet, hashlib.sha256).hexdigest()
            if not hmac.compare_digest(signature, self.headers.get('X-Echo-Signature', '')):
                return self.error(403, 'Invalid device signature')
            if self.path == '/echo-hall/v1':
                try:
                    from hall_light import dispatch
                    response = dispatch(packet)
                except Exception:
                    return self.error(400, 'Hall request unavailable')
                return self.respond(200, response, 'application/octet-stream')
            result = subprocess.run([sys.executable, '-I',
                 str(root/'echo_home.py')], input=packet, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL,
                 timeout=100, creationflags=getattr(subprocess, 'CREATE_NO_WINDOW', 0))
            if result.returncode or not result.stdout:
                return self.error(400, 'Dashboard request unavailable')
            self.respond(200, result.stdout, 'application/octet-stream')
        except (OSError, ValueError, subprocess.TimeoutExpired):
            self.error(503, 'Dashboard unavailable')

    def write_chunk(self, pcm):
        self.wfile.write(('%x\r\n' % len(pcm)).encode('ascii'))
        self.wfile.write(pcm)
        self.wfile.write(b'\r\n')


def main():
    # Support isolated Python (-I), loading sibling modules from this directory.
    root = Path(__file__).resolve().parent
    sys.path.insert(0, str(root))
    parser = argparse.ArgumentParser()
    parser.add_argument('--bind', default='127.0.0.1')
    parser.add_argument('--port', type=int, default=50022)
    parser.add_argument('--engine-port', type=int, default=50021)
    parser.add_argument('--allowed-network', default='127.0.0.1/32')
    parser.add_argument('--keep-awake', action='store_true', help='Prevent automatic idle sleep while this process is serving; no power-plan changes')
    args = parser.parse_args()
    bind = ipaddress.ip_address(args.bind)
    network = ipaddress.ip_network(args.allowed_network)
    if bind.version != 4 or not (bind.is_loopback or bind in network):
        parser.error('--bind must be loopback or an address in --allowed-network')
    if not 1 <= args.port <= 65535 or not 1 <= args.engine_port <= 65535:
        parser.error('invalid port')
    if not os.environ.get('SPEECH_BRIDGE_TOKEN'):
        parser.error('set SPEECH_BRIDGE_TOKEN in the environment')
    if (root/'home-local.json').exists():
        from hall_light import warm
        threading.Thread(target=warm, daemon=True).start()
    logging.basicConfig(level=logging.INFO, format='%(asctime)s %(message)s')
    with SleepGuard(args.keep_awake) as guard:
        server = Server((args.bind, args.port), Voicevox(args.engine_port), args.allowed_network, guard)
        LOG.info('Speech bridge listening on %s:%s; idle sleep prevention=%s', args.bind, args.port, guard.active)
        try:
            server.serve_forever(poll_interval=0.5)
        except KeyboardInterrupt:
            pass
        finally:
            server.server_close()


if __name__ == '__main__':
    main()
