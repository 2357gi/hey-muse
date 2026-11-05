"""Offline corpus evaluation against Jev. This process cannot dispatch a device command."""
import http.client
import json
import sys
import time
from pathlib import Path

examples = [
    ('廊下を明るくして','on'), ('廊下の明かりを落として真っ暗にして','off'),
    ('廊下の灯りを点灯してほしい','on'), ('廊下のライトの電源を切って','off'),
    ('廊下の電気は消さないで','hold'), ('廊下の電気はついてる？','hold'),
    ('電気をつけて','hold'), ('廊下と寝室の照明を消して','hold'),
    ('廊下の電気を消してって言ったらどうなる？','hold'), ('暗いね','hold'),
    ('廊下の電気をつけて','on'), ('廊下の電気を消して','off'),
]
question = {'type':'choice','instructions':'現在の発話が廊下の照明1台への明示的・無条件の電源操作依頼か判定する。質問、否定、引用、複数の対象、明るさ調整や不確かな場合はhold。',
            'criteria':{'on':'廊下の照明だけを今点灯する依頼','off':'廊下の照明だけを今消灯する依頼','hold':'その他すべて。操作しない。'}}
conn = http.client.HTTPSConnection('api.typesafe.ai',timeout=10)
key = sys.stdin.readline().strip()
rows = []
for phrase, expected in examples:
    body = json.dumps({'model':'jev-latest','state':phrase,'questions':{'action':question}})
    start=time.monotonic()
    conn.request('POST','/v1/systemone',body,{'Authorization':'Bearer '+key,'Content-Type':'application/json'})
    res=conn.getresponse()
    raw=res.read()
    elapsed=round(time.monotonic()-start,3)
    if res.status != 200:
        print('TypeSafe HTTP',res.status)
        break
    data=json.loads(raw)
    answer=data['answers']['action']
    rows.append(dict(input=phrase,expected=expected,choice=answer['choice'],confidence=answer['confidence'],
                     elapsed_s=elapsed,model=data.get('model')))
    Path(__file__).with_name('hall-jev-measurement.json').write_text(json.dumps(rows,ensure_ascii=False,indent=2),encoding='utf-8')
print(json.dumps([(x['choice'],x['elapsed_s']) for x in rows]))
