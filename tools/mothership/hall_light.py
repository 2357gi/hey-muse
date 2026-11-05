"""Fixed-target SwitchBot v1.1 executor. Credentials stay on the bridge."""
import base64
import hashlib
import hmac
import http.client
import json
import os
import threading
import time
import uuid
from pathlib import Path


class HallLight:
    def __init__(self):
        entries = dict(line.split('=', 1) for line in
                       Path(os.environ.get('SWITCHBOT_CREDENTIALS', str(Path.home() / 'switchbot_token.env'))).read_text(encoding='utf-8-sig').splitlines()
                       if '=' in line and not line.lstrip().startswith('#'))
        entries = {k.strip().lower().removeprefix('export ').removeprefix('switchbot_'): v for k, v in entries.items()}
        self.token, self.secret = (entries[k].strip().strip('"\'') for k in ('token', 'secret'))
        self.conn = http.client.HTTPSConnection('api.switch-bot.com', timeout=6)
        self.lock = threading.Lock()
        self.target = None
        import echo_home
        self.cfg = json.loads((echo_home.ROOT/'home-local.json').read_text(encoding='utf-8'))
        self.retry_after = 0
        self.last_use = 0

    def request(self, path, payload=None, *, collection="devices"):
        with self.lock:
            if time.monotonic() < self.retry_after:
                raise ValueError('rate limited')
            if time.monotonic() - self.last_use > 10:
                self.conn.close()
            self.last_use = time.monotonic()
            stamp, nonce = str(time.time_ns() // 1000000), str(uuid.uuid4())
            mac = hmac.digest(self.secret.encode(), (self.token+stamp+nonce).encode(), hashlib.sha256)
            headers = {'Authorization': self.token, 't': stamp, 'nonce': nonce,
                       'sign': base64.b64encode(mac).decode(), 'Content-Type': 'application/json'}
            try:
                self.conn.request('GET' if payload is None else 'POST', '/v1.1/'+collection+path,
                                  json.dumps(payload) if payload is not None else None, headers)
                res = self.conn.getresponse()
                raw = res.read()
                if res.status == 429:
                    self.retry_after = time.monotonic() + max(60, int(res.getheader('Retry-After') or 86400))
                if res.status != 200:
                    raise ValueError(f'SwitchBot HTTP {res.status}')
                data = json.loads(raw)
                if data.get('statusCode') != 100:
                    raise ValueError(f"SwitchBot code {data.get('statusCode')}")
                return data.get('body')
            except Exception:
                self.conn.close()
                raise

    def discover(self):
        devices = self.request('')['deviceList']
        return [(d['deviceName'], d['deviceType'], d['deviceId']) for d in devices]

    def status(self):
        power = self.request('/'+self.target+'/status').get('power')
        if power not in ('on', 'off'):
            raise ValueError('unreadable power')
        return power

    def set_power(self, direction):
        if not self.target or direction not in ('on', 'off'):
            raise ValueError('invalid target or direction')
        self.request('/'+self.target+'/commands', {'commandType': 'command',
                     'command': {'on':'turnOn','off':'turnOff'}[direction], 'parameter':'default'})


    def prepare(self):
        target = self.cfg.get('hall', {})
        if not target.get('name') or target.get('type') != 'Color Bulb':
            raise ValueError('hall bulb is not configured')
        matches = [i for n, t, i in self.discover() if (n, t) == (target['name'], target['type'])]
        if len(matches) != 1:
            raise ValueError('hall bulb not unique')
        self.target = matches[0]
        return self.status()


class ResidentHall(HallLight):
    def __init__(self):
        super().__init__()
        self.radio = None
        self.dispatch_lock = threading.Lock()

    def prepare(self):
        state = super().prepare()
        from hall_ble import BulbLink
        self.radio = BulbLink(self.target)
        self.radio.start()
        return state

    def set_power(self, direction):
        import logging
        if not self.dispatch_lock.acquire(blocking=False):
            raise ValueError('another command is in progress')
        try:
            started = time.perf_counter()
            if self.radio.set_power(direction):
                route = 'ble'
            else:
                super().set_power(direction)
                route = 'cloud'
            logging.getLogger('speech-bridge').info('Hall accepted direction=%s route=%s elapsed_ms=%d',
                 direction,route,round((time.perf_counter()-started)*1000))
        finally:
            self.dispatch_lock.release()


# Only named, unique devices or scenes can be reached from the voice endpoint.
# A toggle is offered exclusively as a toggle, never substituted for ON or OFF.
DEVICE_COMMANDS = {
    'living': {'on': 'turnOn', 'off': 'turnOff'},
    'fan': {'on': 'turnOn', 'off': 'turnOff'},
    'plug': {'on': 'turnOn', 'off': 'turnOff'},
    'curtain': {'open': 'turnOn', 'close': 'turnOff'},
    'ac': {'on': 'turnOn', 'off': 'turnOff'},
    'vacuum': {'on': 'turnOn', 'off': 'turnOff'},
}
SCENE_ACTIONS = {('monitor', 'toggle'), ('projector', 'toggle')}
VOICE_ACTIONS = {(target, action) for target, actions in DEVICE_COMMANDS.items()
                 for action in actions} | SCENE_ACTIONS


class VoiceAppliances(HallLight):
    def __init__(self):
        super().__init__()
        self.catalog_lock = threading.Lock()
        self.catalogs = {}

    def resolve(self, target, action):
        scene = (target, action) in SCENE_ACTIONS
        collection = 'scenes' if scene else 'devices'
        with self.catalog_lock:
            stamp, catalog = self.catalogs.get(collection, (0, {}))
            if time.monotonic() - stamp >= 300 or collection not in self.catalogs:
                raw = self.request('', collection=collection)
                catalog = {}
                if scene:
                    for item in raw:
                        catalog.setdefault(item['sceneName'], []).append(item['sceneId'])
                else:
                    for item in raw['deviceList'] + raw['infraredRemoteList']:
                        key = (item['deviceName'], item.get('deviceType') or item.get('remoteType'))
                        catalog.setdefault(key, []).append(item['deviceId'])
                self.catalogs[collection] = (time.monotonic(), catalog)
            if scene:
                key = self.cfg.get('voice_scenes', {}).get(target, {}).get(action)
            else:
                definition = self.cfg.get('voice_devices', {}).get(target, {})
                key = (definition.get('name'), definition.get('type'))
            if not key or (not scene and not all(key)):
                raise ValueError('target is not configured')
            ids = catalog.get(key, [])
            if len(ids) != 1:
                raise ValueError('target unavailable or ambiguous')
            return ids[0]

    def execute(self, target, action):
        ident = self.resolve(target, action)
        if (target, action) in SCENE_ACTIONS:
            self.request('/'+ident+'/execute', {}, collection='scenes')
        else:
            command = DEVICE_COMMANDS[target][action]
            self.request('/'+ident+'/commands', {'commandType':'command', 'command':command,
                                                 'parameter':'default'})


_appliances = None
_appliances_lock = threading.Lock()


def appliances():
    global _appliances
    with _appliances_lock:
        if _appliances is None:
            _appliances = VoiceAppliances()
        return _appliances


_executor = None
_executor_lock = threading.Lock()


def executor():
    global _executor
    with _executor_lock:
        if _executor is None:
            light = ResidentHall()
            light.prepare()
            _executor = light
        return _executor


def warm():
    try:
        executor()
    except Exception:
        pass  # Next authenticated request can attempt discovery.


def dispatch(packet):
    from contextlib import closing
    import sqlite3
    import echo_home
    _, cipher, nonce, req = echo_home.unpack(packet)
    direction = req.get('direction')
    ident = req.get('id')
    target = req.get('target')
    hall = req.get('op') == 'hall' and target is None and direction in ('on','off')
    appliance = req.get('op') == 'appliance' and (target, direction) in VOICE_ACTIONS
    if (not (hall or appliance) or
            not isinstance(ident, str) or str(uuid.UUID(ident)) != ident or
            abs(time.time()-req['time']) > 10):
        raise ValueError('invalid hall request')
    with closing(sqlite3.connect(echo_home.ROOT/'home-requests.sqlite', timeout=2)) as db, db:
        db.execute('create table if not exists hall_seen (id text primary key, created integer)')
        db.execute('delete from hall_seen where created < ?', (int(time.time())-86400,))
        db.execute('insert into hall_seen values (?,?)', (ident,int(time.time())))
    # Commit the reservation before dispatch, including on timeout or process loss.
    ok = False
    try:
        if hall:
            executor().set_power(direction)
        else:
            appliances().execute(target, direction)
        ok = True
    except Exception:
        pass
    return echo_home.pack(cipher,nonce,{'ok':ok})
