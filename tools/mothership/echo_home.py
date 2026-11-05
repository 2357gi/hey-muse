"""Encrypted Echo dashboard: direct SwitchBot API; connected tools only for calendars."""
import base64
from contextlib import closing
import json
import os
from pathlib import Path
import queue
import shutil
import sqlite3
import subprocess
import sys
import threading
import time
from datetime import datetime, timedelta, timezone

from cryptography.hazmat.primitives.ciphers.aead import AESGCM

ROOT = Path(__file__).resolve().parent
JST = timezone(timedelta(hours=9))


class Apps:
    def __init__(self, executable):
        if not Path(executable).is_file():
            executable = shutil.which('codex') or str(max(
                (Path(os.environ['LOCALAPPDATA'])/'OpenAI/Codex/bin').glob('*/codex.exe'),
                key=lambda p:p.stat().st_mtime))
        self.proc = subprocess.Popen([executable, 'app-server', '--listen', 'stdio://'],
            stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL,
            text=True, encoding='utf-8', creationflags=getattr(subprocess, 'CREATE_NO_WINDOW', 0))
        self.inbox = queue.Queue()
        self.serial = 0
        threading.Thread(target=self.read, daemon=True).start()

    def read(self):
        for line in self.proc.stdout:
            self.inbox.put(json.loads(line))
        self.inbox.put(None)

    def send(self, obj):
        self.proc.stdin.write(json.dumps(obj) + '\n')
        self.proc.stdin.flush()

    def rpc(self, method, params):
        self.serial += 1
        self.send(dict(id=self.serial, method=method, params=params))
        deadline = time.monotonic() + 28
        while True:
            item = self.inbox.get(timeout=max(.01, deadline-time.monotonic()))
            if item is None:
                raise ValueError('connection closed')
            if 'method' in item and 'id' in item:
                raise ValueError('interactive authorization required')
            if item.get('id') == self.serial:
                return item['result']

    def connect(self):
        self.rpc('initialize', dict(clientInfo=dict(name='echo_home', version='1'),
                 capabilities=dict(experimentalApi=True)))
        self.send(dict(method='initialized'))
        thread = self.rpc('thread/start', dict(ephemeral=True, cwd=str(ROOT.parent),
                          sandbox='read-only', approvalPolicy='never'))
        self.thread = thread['thread']['id']

    def tool(self, name, arguments):
        result = self.rpc('mcpServer/tool/call', dict(threadId=self.thread, server='codex_apps',
                          tool=name, arguments=arguments))
        if result.get('isError'):
            raise ValueError('tool failed')
        return result.get('structuredContent') or json.loads(result['content'][0]['text'])

    def close(self):
        self.proc.stdin.close()
        try:
            self.proc.wait(timeout=2)
        except subprocess.TimeoutExpired:
            self.proc.kill()
            self.proc.wait()


def local_stamp(value):
    stamp = datetime.fromisoformat(value.replace('Z', '+00:00'))
    return stamp.replace(tzinfo=JST) if stamp.tzinfo is None else stamp.astimezone(JST)


def agenda(apps, calendars):
    now = datetime.now(JST)
    limit = now + timedelta(days=7)
    rows = []
    for cal in calendars:
        args = dict(calendar_id=cal['id'], time_min=now.isoformat(), time_max=limit.isoformat(),
                    timezone_str='Asia/Tokyo', max_results=100)
        while True:
            page = apps.tool('google_calendar.search_events', args)
            for ev in page['events']:
                if ev.get('status') == 'cancelled':
                    continue
                start, end = ev['start'], ev['end']
                if isinstance(start, dict):
                    start = start.get('date_time') or start.get('dateTime') or start.get('date')
                if isinstance(end, dict):
                    end = end.get('date_time') or end.get('dateTime') or end.get('date')
                begin, finish = local_stamp(start), local_stamp(end)
                # The calendar connector can return all-day date boundaries as
                # midnight ISO strings without an offset. Timed events carry one.
                all_day = len(start)==10 or (start.endswith('T00:00:00') and
                          end.endswith('T00:00:00') and finish.date() > begin.date())
                if finish > now:
                    rows.append(dict(title=(ev.get('summary') or '予定')[:160], start=begin.isoformat(),
                          end=finish.isoformat(), day=all_day, calendar=cal['name']))
            token = page.get('next_page_token')
            if not token:
                break
            args['next_page_token'] = token
    return sorted(rows,key=lambda e:e['start'])[:24]


# The Echo sends the direction printed on its button. A stale view must never
# invert that direction on the server. SwitchBot IR reports acceptance, not state.


def power_choices(scenes, last, definitions):
    by_name = {}
    for scene in scenes:
        by_name.setdefault(scene['sceneName'], []).append(scene['sceneId'])
    buttons, targets = [], {}
    for device, definition in definitions.items():
        name, icon = definition['label'], definition.get('icon', 'mdi:power')
        toggle, on, off = (definition.get(k) for k in ('toggle', 'on', 'off'))
        keys = {direction: by_name[label][0] for direction, label in
                [('toggle', toggle), ('on', on), ('off', off)] if len(by_name.get(label, [])) == 1}
        direction = 'off' if last.get(device) == 'on' else 'on'
        if 'toggle' in keys:
            direction = 'toggle'
        elif direction not in keys:
            direction = next(iter(keys), '')
        ident = f'power/{device}/{direction or "setup"}'
        label = name + ('電源' if direction == 'toggle' else ' ' + direction.upper() if direction else ' 未設定')
        buttons.append(dict(id=ident, label=label, icon=icon))
        if direction:
            targets[ident] = keys[direction]
    return buttons, targets


def execute_scene(api, ident, scenes=None):
    # Keep the local allowlist at the caller and verify current membership before
    # dispatch. Never retry: a timeout may follow a successful physical action.
    if scenes is None:
        scenes = api.request('', collection='scenes')
    if sum(scene['sceneId'] == ident for scene in scenes) != 1:
        raise ValueError('scene unavailable or ambiguous')
    api.request('/'+ident+'/execute', {}, collection='scenes')


def power(api, definitions, action=None):
    scenes = api.request('', collection='scenes')
    with closing(sqlite3.connect(ROOT/'home-requests.sqlite', timeout=2)) as db, db:
        db.execute('create table if not exists power_last (device text primary key, direction text)')
        if action is not None:
            # Serialize validation, the single dispatch and recording its receipt.
            db.execute('begin immediate')
        last = dict(db.execute('select device,direction from power_last'))
        buttons, targets = power_choices(scenes, last, definitions)
        if action is None or action not in targets:
            return dict(ok=action is None, controls=buttons)
        _, device, direction = action.split('/')
        execute_scene(api, targets[action], scenes)
        # HallLight.request raises unless HTTP 200 and SwitchBot statusCode 100.
        accepted = True
        if accepted and direction in ('on','off'):
            db.execute('insert or replace into power_last values (?,?)', (device, direction))
            last[device] = direction
        return dict(ok=accepted, controls=power_choices(scenes, last, definitions)[0])

def unpack(body):
    cfg = json.loads((ROOT/'home-local.json').read_text(encoding='utf-8'))
    cipher = AESGCM(bytes.fromhex(cfg['key']))
    packet = base64.b64decode(body, validate=True)
    nonce = packet[:12]
    req = json.loads(cipher.decrypt(nonce, packet[12:], b'echo-home-request-1'))
    if abs(time.time()-req['time']) > 120:
        raise ValueError('expired')
    with closing(sqlite3.connect(ROOT/'home-requests.sqlite', timeout=2)) as db, db:
        db.execute('create table if not exists seen (nonce blob primary key, seen_at integer)')
        db.execute('delete from seen where seen_at < ?', (int(time.time())-300,))
        db.execute('insert into seen values (?,?)', (nonce,int(time.time())))
    return cfg, cipher, nonce, req


def process(body):
    cfg, cipher, nonce, req = unpack(body)
    apps = api = None
    try:
        # The bridge invokes this script with -I. Only this trusted sibling
        # directory is added, to reuse the voice path's Token/Secret client.
        sys.path.insert(0, str(Path(__file__).resolve().parent))
        from hall_light import HallLight
        if req['op'] == 'agenda':
            apps = Apps(cfg.get('codex', 'codex'))
            apps.connect()
            result = dict(ok=True, events=agenda(apps,cfg['calendars']))
            if cfg.get('power_controls'):
                try:
                    api = HallLight()
                    result['controls'] = power(api, cfg['power_controls'])['controls']
                except Exception:
                    pass  # Calendar and fixed scene controls still work.
        elif req['op'] == 'environment' and cfg.get('environment_device'):
            api = HallLight()
            status = api.request('/'+cfg['environment_device']+'/status')
            readings = {key: status[key] for key in ('temperature', 'humidity', 'CO2', 'battery')
                        if type(status.get(key)) in (int, float)}
            result = dict(ok=bool(readings), environment=readings)
        elif req['op'] == 'scene' and cfg.get('power_controls') and req.get('id','').startswith('power/'):
            api = HallLight()
            result = power(api, cfg['power_controls'], req['id'])
        elif req['op'] == 'power_status' and cfg.get('power_controls'):
            api = HallLight()
            result = power(api, cfg['power_controls'])
        elif req['op'] == 'scene' and req.get('id') in cfg.get('scenes', []):
            api = HallLight()
            execute_scene(api, req['id'])
            result = dict(ok=True)
        else:
            raise ValueError('unknown request')
    except Exception:
        result = dict(ok=False)
    finally:
        if apps is not None:
            apps.close()
        if api is not None:
            api.conn.close()
    return pack(cipher, nonce, result)


def pack(cipher, nonce, result):
    out = os.urandom(12)
    return base64.b64encode(out+cipher.encrypt(out,json.dumps(result).encode(),b'echo-home-response-1'+nonce))


if __name__ == '__main__':
    try:
        sys.stdout.buffer.write(process(sys.stdin.buffer.read(4097)))
    except Exception:
        sys.exit(1)
