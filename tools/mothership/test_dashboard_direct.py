import base64
from contextlib import closing
import json
import os
from pathlib import Path
import sqlite3
import tempfile
import time
import unittest
from unittest.mock import Mock, patch

from cryptography.hazmat.primitives.ciphers.aead import AESGCM
import echo_home
import hall_light


class DashboardDirectTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.key = os.urandom(32)
        self.cfg = dict(key=self.key.hex(), codex='unused', calendars=[],
                        environment_device='sensor', scenes=['night'], power_controls={
                            'monitor': {'label': 'Display', 'toggle': 'fixture monitor'},
                            'ac': {'label': 'AC', 'on': 'fixture on', 'off': 'fixture off'}})
        (self.root/'home-local.json').write_text(json.dumps(self.cfg))
        self.root_patch = patch.object(echo_home, 'ROOT', self.root)
        self.root_patch.start()
        self.addCleanup(self.root_patch.stop)
        self.api = Mock()
        self.scenes = [dict(sceneId='night', sceneName='fixture night'),
                       dict(sceneId='mon', sceneName='fixture monitor'),
                       dict(sceneId='ac-on', sceneName='fixture on'),
                       dict(sceneId='ac-off', sceneName='fixture off')]
        self.api.request.side_effect = self.reply
        self.api_patch = patch.object(hall_light, 'HallLight', return_value=self.api)
        self.api_patch.start()
        self.addCleanup(self.api_patch.stop)
        self.apps_patch = patch.object(echo_home, 'Apps', side_effect=AssertionError('connector used'))
        self.apps = self.apps_patch.start()
        self.addCleanup(self.apps_patch.stop)

    def reply(self, path, payload=None, *, collection='devices'):
        if collection == 'scenes' and path == '':
            return self.scenes
        if path == '/sensor/status':
            return dict(temperature=25.3, humidity=53, CO2=970, battery=55)
        return {}

    def packet(self, op, ident='', stamp=None):
        nonce = os.urandom(12)
        body = json.dumps(dict(op=op, id=ident, time=time.time() if stamp is None else stamp)).encode()
        return nonce, base64.b64encode(nonce+AESGCM(self.key).encrypt(nonce, body, b'echo-home-request-1'))

    def send(self, op, ident=''):
        nonce, packet = self.packet(op, ident)
        raw = base64.b64decode(echo_home.process(packet))
        return json.loads(AESGCM(self.key).decrypt(raw[:12], raw[12:], b'echo-home-response-1'+nonce))

    def test_environment_does_not_start_connector(self):
        result = self.send('environment')
        self.assertEqual(result, dict(ok=True, environment=dict(temperature=25.3, humidity=53, CO2=970, battery=55)))
        self.api.request.assert_called_once_with('/sensor/status')
        self.apps.assert_not_called()
        self.api.conn.close.assert_called_once()

    def test_all_buttons_resolve_and_only_selected_scene_dispatches(self):
        status = self.send('power_status')
        self.assertTrue(status['ok'])
        self.assertEqual([b['id'] for b in status['controls']], ['power/monitor/toggle', 'power/ac/on'])
        for ident, target in [('night', 'night'), ('power/monitor/toggle', 'mon'), ('power/ac/on', 'ac-on')]:
            with self.subTest(ident=ident):
                self.api.request.reset_mock()
                self.assertTrue(self.send('scene', ident)['ok'])
                writes = [c for c in self.api.request.call_args_list if len(c.args) > 1]
                self.assertEqual(len(writes), 1)
                self.assertEqual(writes[0].args, ('/'+target+'/execute', {}))
                self.assertEqual(writes[0].kwargs, dict(collection='scenes'))
        self.assertEqual(self.send('power_status')['controls'][1]['id'], 'power/ac/off')
        self.apps.assert_not_called()

    def test_unknown_missing_and_ambiguous_scenes_never_dispatch(self):
        self.assertFalse(self.send('scene', 'unconfigured')['ok'])
        self.assertFalse(self.send('scene', 'power/other/on')['ok'])
        self.scenes = [s for s in self.scenes if s['sceneId'] != 'night']
        self.assertFalse(self.send('scene', 'night')['ok'])
        self.scenes += [dict(sceneId='night', sceneName='fixture night')]*2
        self.assertFalse(self.send('scene', 'night')['ok'])
        self.assertFalse(any(len(c.args) > 1 for c in self.api.request.call_args_list))

    def test_uncertain_execution_is_not_retried_or_recorded_as_success(self):
        def timeout(path, payload=None, **kwargs):
            if payload is not None:
                raise TimeoutError('unknown result')
            return self.reply(path, **kwargs)
        self.api.request.side_effect = timeout
        self.assertFalse(self.send('scene', 'power/ac/on')['ok'])
        self.assertEqual(sum(len(c.args) > 1 for c in self.api.request.call_args_list), 1)
        with closing(sqlite3.connect(self.root/'home-requests.sqlite')) as db:
            self.assertEqual(db.execute('select * from power_last').fetchall(), [])
        self.apps.assert_not_called()

    def test_replay_expired_and_tampered_packets_cannot_dispatch(self):
        _, packet = self.packet('scene', 'night')
        echo_home.process(packet)
        self.api.request.reset_mock()
        _, expired = self.packet('scene', 'night', time.time()-121)
        corrupt = bytearray(base64.b64decode(self.packet('scene', 'night')[1]))
        corrupt[-1] ^= 1
        for invalid in (packet, expired, base64.b64encode(corrupt)):
            with self.assertRaises(Exception):
                echo_home.process(invalid)
        self.api.request.assert_not_called()

    def test_calendar_keeps_its_connector(self):
        apps = Mock()
        self.apps.side_effect = None
        self.apps.return_value = apps
        result = self.send('agenda')
        self.assertTrue(result['ok'])
        self.assertEqual(len(result['controls']), 2)
        apps.connect.assert_called_once()
        apps.close.assert_called_once()


if __name__ == '__main__':
    unittest.main()
