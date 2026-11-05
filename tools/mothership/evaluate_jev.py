"""Explicit live evaluation using synthetic phrases; never dispatches a view or device action."""
import argparse
import json
import statistics
import time
from pathlib import Path

from jev_intent import classify


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--model', required=True)
    parser.add_argument('--cases', type=Path, default=Path(__file__).with_name('jev_cases.json'))
    parser.add_argument('--output', type=Path, default=Path(__file__).with_name('jev-evaluation.json'))
    args = parser.parse_args()
    cases = json.loads(args.cases.read_text(encoding='utf-8'))
    rows = []
    for text, expected in cases:
        start = time.perf_counter()
        result = classify(text, {'enabled': True, 'model': args.model})
        rows.append({'text': text, 'expected': expected, **result, 'seconds': round(time.perf_counter()-start, 3)})
    correct = sum(row['intent'] == row['expected'] and row['ok'] for row in rows)
    false_routes = sum(row['intent'] != 'none' and row['intent'] != row['expected'] for row in rows)
    summary = {'model': args.model, 'cases': len(rows), 'correct': correct, 'false_routes': false_routes,
               'unavailable': sum(not row['ok'] for row in rows), 'median_seconds': statistics.median(row['seconds'] for row in rows)}
    args.output.write_text(json.dumps({'summary': summary, 'rows': rows}, ensure_ascii=False, indent=2), encoding='utf-8')
    print(json.dumps(summary))
    for row in rows:
        if row['intent'] != row['expected'] or not row['ok']:
            print(json.dumps(row, ensure_ascii=False))
    if false_routes or any(not row['ok'] for row in rows):
        raise SystemExit(1)


if __name__ == '__main__':
    main()
