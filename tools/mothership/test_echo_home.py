import base64
import json
import os
import tempfile
import time
import unittest
from datetime import datetime, timedelta
from pathlib import Path
from unittest.mock import Mock, patch

from cryptography.exceptions import InvalidTag
from cryptography.hazmat.primitives.ciphers.aead import AESGCM

import echo_home
import hall_light
import speech_bridge


class HomeTests(unittest.TestCase):
    def test_calendar_pages_and_all_day_boundaries(self):
        tomorrow = (datetime.now(echo_home.JST) + timedelta(days=1)).date()
        end = tomorrow + timedelta(days=1)
        apps = Mock()
        apps.tool.side_effect = [
            {'events': [{'summary': 'fixture all-day', 'start': {'date': str(tomorrow)},
                         'end': {'date': str(end)}}], 'next_page_token': 'page2'},
            {'events': [{'summary': 'cancelled', 'status': 'cancelled'},
                        {'summary': 'fixture timed', 'start': f'{tomorrow}T10:00:00+09:00',
                         'end': f'{tomorrow}T11:00:00+09:00'}]},
        ]
        events = echo_home.agenda(apps, [{'id': 'fixture-calendar', 'name': 'Test'}])
        self.assertEqual([row['day'] for row in events], [True, False])
        self.assertEqual(apps.tool.call_count, 2)
        self.assertEqual(apps.tool.call_args.args[1]['next_page_token'], 'page2')

    def test_power_targets_only_come_from_local_definitions(self):
        scenes = [{'sceneName': 'fixture-on', 'sceneId': 'scene1'},
                  {'sceneName': 'fixture-off', 'sceneId': 'scene2'}]
        self.assertEqual(echo_home.power_choices(scenes, {}, {}), ([], {}))
        definitions = {'monitor': {'label': 'Display', 'on': 'fixture-on', 'off': 'fixture-off'}}
        buttons, targets = echo_home.power_choices(scenes, {}, definitions)
        self.assertEqual(targets, {'power/monitor/on': 'scene1'})
        self.assertEqual(buttons[0]['id'], 'power/monitor/on')
        _, targets = echo_home.power_choices(scenes, {'monitor': 'on'}, definitions)
        self.assertEqual(targets, {'power/monitor/off': 'scene2'})
        _, targets = echo_home.power_choices(scenes + [scenes[0]], {}, definitions)
        self.assertNotIn('power/monitor/on', targets)

    def test_unconfigured_voice_device_cannot_dispatch(self):
        import threading
        api = hall_light.VoiceAppliances.__new__(hall_light.VoiceAppliances)
        api.cfg = {}
        api.catalog_lock = threading.Lock()
        api.catalogs = {}
        api.request = Mock(return_value={'deviceList': [], 'infraredRemoteList': []})
        with self.assertRaises(ValueError):
            api.execute('living', 'off')
        api.request.assert_called_once_with('', collection='devices')

    def test_dashboard_authentication_expiry_and_replay(self):
        key = os.urandom(32)
        def packet(stamp):
            nonce = os.urandom(12)
            body = json.dumps({'op': 'agenda', 'time': stamp}).encode()
            return base64.b64encode(nonce + AESGCM(key).encrypt(nonce, body, b'echo-home-request-1'))
        with tempfile.TemporaryDirectory() as tmp, patch.object(echo_home, 'ROOT', Path(tmp)):
            (Path(tmp)/'home-local.json').write_text(json.dumps({'key': key.hex()}))
            body = packet(time.time())
            _, cipher, nonce, _ = echo_home.unpack(body)
            response = base64.b64decode(echo_home.pack(cipher, nonce, {'ok': True}))
            self.assertEqual(json.loads(cipher.decrypt(response[:12], response[12:],
                             b'echo-home-response-1' + nonce)), {'ok': True})
            with self.assertRaises(Exception):
                echo_home.unpack(body)
            with self.assertRaises(ValueError):
                echo_home.unpack(packet(time.time() - 121))
            corrupt = bytearray(base64.b64decode(packet(time.time())))
            corrupt[-1] ^= 1
            with self.assertRaises(InvalidTag):
                echo_home.unpack(base64.b64encode(corrupt))

    def test_speech_service_requires_configured_token(self):
        with patch.dict(os.environ, {}, clear=True), self.assertRaises(ValueError):
            speech_bridge.Server(('127.0.0.1', 0), Mock(), '127.0.0.1/32')
