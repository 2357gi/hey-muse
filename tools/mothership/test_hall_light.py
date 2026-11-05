import base64
import json
import os
import tempfile
import time
import unittest
import uuid
from pathlib import Path
from unittest.mock import patch, Mock
from cryptography.hazmat.primitives.ciphers.aead import AESGCM
import hall_light
import echo_home


class ExecutionTest(unittest.TestCase):
    def test_duplicate_expiry_and_invalid_target(self):
        key = os.urandom(32)
        ident = str(uuid.uuid4())
        def packet(**updates):
            req = {'op':'hall','direction':'off','time':int(time.time()),'id':ident,**updates}
            nonce = os.urandom(12)
            return base64.b64encode(nonce+AESGCM(key).encrypt(nonce,json.dumps(req).encode(),b'echo-home-request-1'))
        light = Mock()
        with tempfile.TemporaryDirectory() as tmp, patch.object(echo_home,'ROOT',Path(tmp)), patch.object(hall_light,'executor',return_value=light):
            (Path(tmp)/'home-local.json').write_text(json.dumps({'key':key.hex()}))
            p = packet()
            hall_light.dispatch(p)
            for invalid in [p, packet(), packet(direction='toggle',id=str(uuid.uuid4())),
                            packet(time=int(time.time())-20,id=str(uuid.uuid4())),packet(op='scene')]:
                with self.assertRaises(Exception):
                    hall_light.dispatch(invalid)
            light.set_power.assert_called_once_with('off')


    def test_uncertain_ble_write_never_replays_via_cloud(self):
        import threading
        link=object.__new__(hall_light.ResidentHall)
        link.dispatch_lock=threading.Lock()
        link.radio=Mock()
        with patch.object(hall_light.HallLight,'set_power') as cloud:
            link.radio.set_power.side_effect=TimeoutError()
            with self.assertRaises(TimeoutError):link.set_power('off')
            cloud.assert_not_called()
            link.radio.set_power.side_effect=None
            link.radio.set_power.return_value=False
            link.set_power('off')
            cloud.assert_called_once_with('off')

    def test_appliance_wire_commands_and_unique_names(self):
        api = hall_light.VoiceAppliances.__new__(hall_light.VoiceAppliances)
        import threading
        api.catalog_lock = threading.Lock()
        api.catalogs = {}
        api.cfg = {'voice_devices': {'curtain': {'name': 'fixture curtain', 'type': 'Curtain3'},
                                    'living': {'name': 'fixture light', 'type': 'DIY Light'}},
                   'voice_scenes': {'monitor': {'toggle': 'fixture monitor'}}}
        inventory = {'deviceList':[{'deviceName':'fixture curtain','deviceType':'Curtain3','deviceId':'cur'}],
                     'infraredRemoteList':[{'deviceName':'fixture light','remoteType':'DIY Light','deviceId':'light'}]}
        def reply(path, payload=None, *, collection='devices'):
            if path:return {}
            if collection=='scenes':return [{'sceneName':'fixture monitor','sceneId':'mon'}]
            return inventory
        api.request = Mock(side_effect=reply)
        api.execute('curtain','close')
        api.request.assert_called_with('/cur/commands',{'commandType':'command','command':'turnOff','parameter':'default'})
        api.execute('living','on')
        api.request.assert_called_with('/light/commands',{'commandType':'command','command':'turnOn','parameter':'default'})
        api.execute('monitor','toggle')
        api.request.assert_called_with('/mon/execute',{},collection='scenes')
        inventory['deviceList'] *= 2
        api.catalogs.clear()
        with self.assertRaises(ValueError):api.execute('curtain','open')

    def test_encrypted_appliance_toggle_cannot_be_replayed(self):
        key = os.urandom(32)
        ident = str(uuid.uuid4())
        api=Mock()
        with tempfile.TemporaryDirectory() as tmp, patch.object(echo_home,'ROOT',Path(tmp)), patch.object(hall_light,'appliances',return_value=api):
            (Path(tmp)/'home-local.json').write_text(json.dumps({'key':key.hex()}))
            def send(target,action):
                req={'op':'appliance','direction':action,'target':target,'time':int(time.time()),'id':ident}
                n=os.urandom(12)
                return hall_light.dispatch(base64.b64encode(n+AESGCM(key).encrypt(n,json.dumps(req).encode(),b'echo-home-request-1')))
            with self.assertRaises(ValueError):send('monitor','on')
            with self.assertRaises(ValueError):send('mirror','toggle')
            send('monitor','toggle')
            with self.assertRaises(Exception):send('monitor','toggle')
            api.execute.assert_called_once_with('monitor','toggle')
