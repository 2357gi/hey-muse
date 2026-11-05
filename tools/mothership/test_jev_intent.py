import base64
import copy
import hashlib
import hmac
import http.client
import json
import os
import secrets
import socket
import tempfile
import threading
import time
import unittest
from pathlib import Path
from unittest.mock import Mock, patch

from cryptography.hazmat.primitives.ciphers.aead import AESGCM
import echo_home
import jev_intent
import speech_bridge


def answer(intent='weather_today', yes=0.99, probability=0.98):
    values = {key: 0.0 for key in jev_intent.INTENTS}
    values[intent], values['none'] = probability, 1-probability
    return {'answers': {'supported': {'type': 'noul', 'noul': yes},
            'intent': {'type': 'choice', 'choice': intent, 'probabilities': values, 'confidence': 0.9}}}


class IntentTests(unittest.TestCase):
    def test_every_supported_view_and_uncertainty(self):
        for intent in jev_intent.INTENTS:
            if intent != 'none':
                self.assertEqual(jev_intent.select_intent(answer(intent)), intent)
        self.assertEqual(jev_intent.select_intent(answer(yes=0.4)), 'none')
        self.assertEqual(jev_intent.select_intent(answer(probability=0.6)), 'none')

    def test_invalid_outputs_never_become_actions(self):
        for value in (float('nan'), float('inf'), -1, 1.1, '0.9', True):
            with self.subTest(value=value), self.assertRaises(ValueError):
                jev_intent.select_intent(answer(yes=value))
        for invalid in [None, [], {'answers': None}, {'answers': {'supported': None, 'intent': None}}]:
            with self.assertRaises(ValueError):
                jev_intent.select_intent(invalid)
        data = answer()
        for key, value in [('choice', 'hall_off'), ('type', 'text'), ('probabilities', {'weather_today': 1})]:
            modified = copy.deepcopy(data)
            modified['answers']['intent'][key] = value
            with self.assertRaises(ValueError):
                jev_intent.select_intent(modified)

    def test_disabled_missing_key_and_invalid_input_do_not_call_provider(self):
        with patch.dict(os.environ, {}, clear=True), patch.object(jev_intent.http.client, 'HTTPSConnection') as connection:
            for text, settings in [('雨降る？', {}), ('雨降る？', {'enabled': True}),
                                   ('x'*321, {'enabled': True}), ('', {'enabled': True})]:
                self.assertFalse(jev_intent.classify(text, settings)['ok'])
            connection.assert_not_called()

    def test_request_contains_only_utterance_and_key_comes_from_environment(self):
        token = secrets.token_urlsafe(32)
        response = Mock(status=200)
        response.read.return_value = json.dumps(answer()).encode()
        conn = Mock()
        conn.getresponse.return_value = response
        with patch.dict(os.environ, {'TYPESAFE_API_KEY': token}), patch.object(jev_intent.http.client, 'HTTPSConnection', return_value=conn):
            result = jev_intent.classify('雨降りそう？', {'enabled': True, 'model': 'test-model'})
        self.assertEqual(result, {'ok': True, 'intent': 'weather_today'})
        args = conn.request.call_args.args
        data = json.loads(args[2])
        self.assertEqual(data['state'], {'utterance': '雨降りそう？'})
        self.assertEqual(data['model'], 'test-model')
        self.assertEqual(args[3]['Authorization'], 'Bearer '+token)
        self.assertNotIn(token, json.dumps(result))
        conn.close.assert_called_once()

    def test_key_file_and_provider_failures_are_not_exposed_or_retried(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp)/'key'
            token = secrets.token_urlsafe(32)
            path.write_text(token)
            with patch.dict(os.environ, {'TYPESAFE_API_KEY_FILE': str(path)}, clear=True):
                self.assertEqual(jev_intent.api_key(), token)
                for status, body in [(401, token.encode()), (429, b'limit'), (503, b'outage'), (302, b'redirect'),
                                     (200, b'{'), (200, b'x'*32769), (200, b'{}')]:
                    conn = Mock()
                    conn.getresponse.return_value = Mock(status=status, read=Mock(return_value=body))
                    with patch.object(jev_intent.http.client, 'HTTPSConnection', return_value=conn):
                        self.assertEqual(jev_intent.classify('雨？', {'enabled': True}), {'ok': False, 'intent': 'none'})
                    self.assertEqual(conn.request.call_count, 1)
                conn = Mock()
                conn.request.side_effect = socket.timeout()
                with patch.object(jev_intent.http.client, 'HTTPSConnection', return_value=conn):
                    self.assertFalse(jev_intent.classify('雨？', {'enabled': True})['ok'])
                conn.close.assert_called_once()

    def test_authenticated_http_endpoint_is_read_only_and_replay_protected(self):
        key = os.urandom(32)
        with tempfile.TemporaryDirectory() as tmp, patch.object(echo_home, 'ROOT', Path(tmp)):
            (Path(tmp)/'home-local.json').write_text(json.dumps({'key': key.hex(), 'jev': {'enabled': True}}))
            server = speech_bridge.Server(('127.0.0.1', 0), Mock(), '127.0.0.1/32',
                                          token=secrets.token_urlsafe(32), home_root=Path(tmp))
            worker = threading.Thread(target=server.serve_forever, daemon=True)
            worker.start()
            def packet(op='classify_read_only'):
                nonce = os.urandom(12)
                payload = json.dumps({'op': op, 'time': time.time(), 'text': '明日雨かな？'}).encode()
                return nonce, base64.b64encode(nonce+AESGCM(key).encrypt(nonce, payload, b'echo-home-request-1'))
            def post(body, signature=True):
                conn = http.client.HTTPConnection(*server.server_address, timeout=3)
                headers = {'X-Echo-Signature': hmac.new(key, body, hashlib.sha256).hexdigest()} if signature else {}
                conn.request('POST', '/echo-intent/v1', body, headers)
                response = conn.getresponse()
                status, data = response.status, response.read()
                conn.close()
                return status, data
            try:
                with patch.object(jev_intent, 'classify', return_value={'ok': True, 'intent': 'weather_tomorrow'}) as classify, \
                     patch.object(echo_home, 'Apps', side_effect=AssertionError('connector must not run')):
                    nonce, body = packet()
                    self.assertEqual(post(body, False)[0], 403)
                    status, raw = post(body)
                    self.assertEqual(status, 200)
                    raw = base64.b64decode(raw)
                    result = json.loads(AESGCM(key).decrypt(raw[:12], raw[12:], b'echo-home-response-1'+nonce))
                    self.assertEqual(result['intent'], 'weather_tomorrow')
                    self.assertEqual(post(body)[0], 400)
                    self.assertEqual(post(packet('scene')[1])[0], 400)
                    classify.assert_called_once()
            finally:
                server.shutdown()
                server.server_close()
                worker.join()
