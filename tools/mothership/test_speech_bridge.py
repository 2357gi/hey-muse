import importlib.util
import http.client
import io
import json
import os
import pathlib
import secrets
import threading
import time
import unittest
import urllib.error
import urllib.request
import wave

ROOT = pathlib.Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location('speech_bridge', ROOT / 'speech_bridge.py')
bridge = importlib.util.module_from_spec(spec)
spec.loader.exec_module(bridge)
RESULT = ROOT / 'test-result.json'
TEST_TOKEN = secrets.token_urlsafe(32)


class FakeEngine:
    def __init__(self):
        self.calls = []
        self.ready = True

    def health(self):
        return self.ready

    def synthesize(self, text, speaker):
        self.calls.append((text, speaker))
        return b'\x00\x00\x01\x00\xff\xff\x00\x40' * 100


class ContractTests(unittest.TestCase):
    def setUp(self):
        self.backend = FakeEngine()
        self.server = bridge.Server(('127.0.0.1', 0), self.backend, '192.0.2.0/24', token=TEST_TOKEN)
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.thread.start()
        self.url = 'http://127.0.0.1:' + str(self.server.server_port)

    def tearDown(self):
        self.server.shutdown()
        self.server.server_close()
        self.thread.join()

    def send(self, body, key=TEST_TOKEN, path='/v1/audio/speech', headers=None):
        if isinstance(body, dict):
            body = json.dumps(body, ensure_ascii=False).encode()
        req = urllib.request.Request(self.url + path, data=body, headers={
            'Content-Type': 'application/json', 'Authorization': 'Bearer ' + key, **(headers or {})})
        try:
            with urllib.request.urlopen(req, timeout=5) as response:
                return response.status, response.headers, response.read()
        except urllib.error.HTTPError as error:
            return error.code, error.headers, error.read()

    def request_body(self, **changes):
        return dict(model='voicevox', voice='alloy', input='音声の準備を確認します。', response_format='pcm') | changes

    def test_hey_muse_request_returns_raw_pcm_and_maps_voice(self):
        code, headers, pcm = self.send(self.request_body(), path='/audio/speech')
        self.assertEqual(code, 200)
        self.assertEqual(headers['Content-Type'], 'application/octet-stream')
        self.assertEqual(len(pcm), 800)
        self.assertFalse(pcm.startswith(b'RIFF'))
        self.assertEqual(self.backend.calls, [('音声の準備を確認します。', 3)])

    def test_bad_authorization_and_wrong_host_never_reach_engine(self):
        self.assertEqual(self.send(self.request_body(), key='')[0], 401)
        self.assertEqual(self.send(self.request_body(), headers={'Host': 'untrusted.example'})[0], 403)
        self.assertEqual(self.backend.calls, [])

    def test_clock_times_get_spoken_units_in_each_audio_part(self):
        text = '２０：２０に開始。09:05から10:00まで。24:00に終了。'
        self.assertEqual(self.send(self.request_body(input=text))[0], 200)
        self.assertEqual([s for s, _ in self.backend.calls],
                         ['20時20分に開始。', '9時5分から10時まで。', '24時に終了。'])
        untouched = '25:70 120:20 10:20:30 127.0.0.1:20'
        self.assertEqual(self.send(self.request_body(input=untouched))[0], 200)
        self.assertEqual(self.backend.calls[-1], (untouched, 3))

    def test_first_audio_is_readable_while_next_sentence_is_still_synthesizing(self):
        release = threading.Event()
        waiting = threading.Event()
        words = ['「最初です。」', '次は3.14です。', '終わり！']
        def synthesize(text, speaker):
            self.backend.calls.append((text, speaker))
            if text == words[1]:
                waiting.set()
                if not release.wait(4):
                    raise RuntimeError('first sentence was buffered')
            return bytes([words.index(text) + 1, 0]) * 400
        self.backend.synthesize = synthesize
        req = urllib.request.Request(self.url + '/v1/audio/speech',
            json.dumps(self.request_body(input='\n'.join(words))).encode(),
            headers={'Content-Type': 'application/json', 'Authorization': 'Bearer ' + TEST_TOKEN})
        try:
            with urllib.request.urlopen(req, timeout=5) as response:
                self.assertTrue(waiting.wait(2))
                self.assertEqual(response.read(800), b'\x01\x00' * 400)
                self.assertEqual(self.send(self.request_body())[0], 503)
                release.set()
                self.assertEqual(response.read(), b'\x02\x00' * 400 + b'\x03\x00' * 400)
        finally:
            release.set()
        self.assertEqual(self.backend.calls, [(word, 3) for word in words])

    def test_late_synthesis_failure_is_not_successful_end_of_audio(self):
        def synthesize(text, speaker):
            if text == '後半。':
                raise bridge.BackendUnavailable('engine unavailable')
            return b'\x01\x00' * 400
        self.backend.synthesize = synthesize
        with self.assertRaises(http.client.IncompleteRead):
            self.send(self.request_body(input='前半。後半。'))

    def test_invalid_audio_contract_and_oversize_rejected_before_synthesis(self):
        for field, value in [('response_format', 'wav'), ('model', 'unknown'), ('voice', []), ('input', 'x' * 1001)]:
            body = self.request_body()
            body[field] = value
            self.assertEqual(self.send(body)[0], 400)
        self.assertEqual(self.send(b'{' + b' ' * 33000)[0], 413)
        self.assertEqual(self.send(b'{}', headers={'Content-Length': '9' * 100})[0], 413)
        self.assertEqual(self.backend.calls, [])

    def test_backend_failure_is_non_200_and_health_is_503(self):
        self.backend.ready = False
        with self.assertRaises(urllib.error.HTTPError) as error:
            urllib.request.urlopen(self.url + '/health')
        self.assertEqual(error.exception.code, 503)
        def fail(*_args):
            raise bridge.BackendUnavailable('Local VOICEVOX Engine is unavailable')
        self.backend.synthesize = fail
        code, _, _ = self.send(self.request_body())
        self.assertEqual(code, 503)

    def test_wav_converter_rejects_stereo_wrong_rate_and_truncated_payload(self):
        for channels, rate in [(2, 24000), (1, 48000), (1, 24000)]:
            stream = io.BytesIO()
            with wave.open(stream, 'wb') as output:
                output.setnchannels(channels)
                output.setsampwidth(2)
                output.setframerate(rate)
                output.writeframes(b'\x01\x00' * 100 * channels)
            data = stream.getvalue()
            if rate == 24000 and channels == 1:
                self.assertEqual(bridge.pcm_from_wav(data), b'\x01\x00' * 100)
                data = data[:-1]
            with self.assertRaises(bridge.BackendUnavailable):
                bridge.pcm_from_wav(data)


class SleepGuardTests(unittest.TestCase):
    def test_exception_restores_previous_thread_state_and_failures_are_explicit(self):
        state = [bridge.SleepGuard.CONTINUOUS]
        def setter(flags):
            previous, state[0] = state[0], flags
            return previous
        guard = bridge.SleepGuard(True, setter)
        with self.assertRaisesRegex(RuntimeError, 'sample failure'):
            with guard:
                self.assertTrue(state[0] & bridge.SleepGuard.SYSTEM_REQUIRED)
                raise RuntimeError('sample failure')
        self.assertFalse(guard.active)
        self.assertEqual(state[0], bridge.SleepGuard.CONTINUOUS)
        with self.assertRaises(OSError):
            with bridge.SleepGuard(True, lambda _flags: 0):
                pass

    @unittest.skipUnless(os.name == 'nt', 'Windows API test')
    def test_real_windows_thread_request_is_set_and_released(self):
        guard = bridge.SleepGuard(True)
        with guard:
            self.assertTrue(guard.active)
            observed = guard.set_state(guard.CONTINUOUS | guard.SYSTEM_REQUIRED)
            self.assertTrue(observed & guard.SYSTEM_REQUIRED)
        self.assertFalse(guard.active)
        after = guard.set_state(guard.CONTINUOUS)
        try:
            self.assertEqual(after, guard.previous | guard.CONTINUOUS)
        finally:
            guard.set_state(guard.previous | guard.CONTINUOUS)


def live_test():
    backend = bridge.Voicevox()
    if not backend.health():
        raise RuntimeError('Real VOICEVOX 0.26.0 is not ready')
    server = bridge.Server(('127.0.0.1', 0), backend, '192.0.2.0/24', token=TEST_TOKEN)
    worker = threading.Thread(target=server.serve_forever, daemon=True)
    worker.start()
    try:
        started = time.monotonic()
        req = urllib.request.Request(f'http://127.0.0.1:{server.server_port}/v1/audio/speech',
            data=json.dumps({'model': 'voicevox', 'voice': 'alloy', 'input': '母艦の音声準備を確認しました。',
                             'response_format': 'pcm', 'instructions': ''}, ensure_ascii=False).encode(),
            headers={'Content-Type': 'application/json', 'Authorization': 'Bearer ' + TEST_TOKEN})
        with urllib.request.urlopen(req, timeout=90) as response:
            pcm = response.read()
            content_type = response.headers['Content-Type']
            status = response.status
        if status != 200 or len(pcm) < 4800 or len(pcm) % 2 or pcm.startswith(b'RIFF') or not any(pcm):
            raise RuntimeError('Invalid real PCM response')
        (ROOT / 'test-zundamon.pcm').write_bytes(pcm)
        with wave.open(str(ROOT / 'test-zundamon.wav'), 'wb') as wav:
            wav.setnchannels(1)
            wav.setsampwidth(2)
            wav.setframerate(24000)
            wav.writeframes(pcm)
        return {'status': status, 'content_type': content_type, 'pcm_bytes': len(pcm),
                'rate': 24000, 'channels': 1, 'bits': 16, 'seconds': len(pcm)/48000,
                'elapsed_seconds': round(time.monotonic()-started, 3), 'speaker': 3,
                'engine': '0.26.0', 'scope': 'MotherShip loopback; no Echo; no SDK credentials'}
    finally:
        server.shutdown()
        server.server_close()
        worker.join()


if __name__ == '__main__':
    suite = unittest.TestSuite([
        unittest.defaultTestLoader.loadTestsFromTestCase(ContractTests),
        unittest.defaultTestLoader.loadTestsFromTestCase(SleepGuardTests),
    ])
    run = unittest.TextTestRunner(verbosity=2).run(suite)
    if not run.wasSuccessful():
        raise SystemExit(1)
    result = {'contract_tests': run.testsRun, 'passed': True, 'live': live_test()}
    RESULT.write_text(json.dumps(result, indent=2), encoding='utf-8')
    print(json.dumps(result, indent=2))
