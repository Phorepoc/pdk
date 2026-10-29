# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0

"""phone_gate -- the regression gate after a push to a phone: PASS only if the running image is a recorded pipeline build
and the UI behaves (no white frames, GPU composition up, real transition effects). Run it after every push, before
reporting a phone result; it drives the phone through PhoneLab's REST API.

    python3 tools/phone_gate.py [--api http://127.0.0.1:8737] [--device pinephone] [--quick]

Checks:
  1. provenance  the last push into this boot carried a clean build record (PhoneLab's push result)
  2. markers     this boot reached the home screen, "wfc gpu: GPU composition up", the real transition effects
                 (CGfxTransAdapterTfx), the display's frame log (the [mon] line's frames= counter)
  3. scenario    menu open + scrolling + back, screen off, a tap on the dark screen, screen on; during it the display
                 driver's frame log must report no WHITE frame after a non-white one, and the frame a screen-on shows
                 must not be mostly white
Exit 0 = PASS, 1 = FAIL (each failed check printed), 2 = PhoneLab unreachable.
"""
import argparse
import json
import re
import sys
import time
import urllib.parse
import urllib.request


class Lab:
    def __init__(self, api, dev):
        self.api, self.dev = api.rstrip('/'), dev

    def get(self, path, **q):
        url = '%s/api/devices/%s' % (self.api, self.dev) + ('/' + path if path else '')
        if q:
            url += '?' + urllib.parse.urlencode(q)
        with urllib.request.urlopen(url, timeout=60) as r:
            return json.loads(r.read())

    def op(self, name, **args):
        req = urllib.request.Request('%s/api/devices/%s/ops/%s' % (self.api, self.dev, name),
                                     data=json.dumps(args).encode(), headers={'Content-Type': 'application/json'})
        try:
            with urllib.request.urlopen(req, timeout=300) as r:
                return json.loads(r.read())
        except urllib.error.HTTPError as e:
            return json.loads(e.read() or b'{}')

    def lines(self, after=0, any_=None, limit=2000):
        q = {'boot': 'current', 'limit': limit}
        if after:
            q['after'] = after
        if any_:
            q['any'] = '|'.join(any_)
        return self.get('lines', **q)

    def mark(self):
        out = self.lines(limit=1)
        return out[-1]['id'] if out else 0


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--api', default='http://127.0.0.1:8737')
    ap.add_argument('--device', default='pinephone')
    ap.add_argument('--quick', action='store_true', help='markers + provenance only, no scenario')
    a = ap.parse_args()
    lab = Lab(a.api, a.device)
    fails = []

    try:
        dev = lab.get('')
    except Exception as e:  # noqa: BLE001 -- any transport error = unreachable
        print('phone_gate: PhoneLab unreachable: %s' % e)
        return 2
    boot = (dev.get('health') or {}).get('boot') if isinstance(dev, dict) else None
    print('phone_gate: %s boot %s' % (a.device, boot))

    # 1. provenance: the newest push, and whether this boot came after it
    hist = lab.get('history', limit=400)
    hist = hist.get('items', hist) if isinstance(hist, dict) else hist
    pushes = [h for h in hist if h.get('op') == 'push' and isinstance(h.get('result'), dict)]
    if not pushes:
        fails.append('provenance: no push in the recent history')
    else:
        prov = pushes[0]['result'].get('provenance')
        if not isinstance(prov, dict):
            fails.append('provenance: the last push carried no build record (unverified or a stale image)')
        else:
            print('  build: board %s, %s' % (prov.get('board'), ', '.join('%s %s' % (k, v[:9]) for k, v in
                                                                         sorted(prov.get('repos', {}).items()))))

    # 2. markers of this boot
    need = {
        'the pushed image (slot B)': 'running slot B',
        'home screen': 'homescreen=1',
        # (either line: the start-up line or the composer's per-frame report -- a serial line can arrive garbled)
        'GPU composition': ('wfc gpu: GPU composition up', 'wfc gpu: composition '),
        'transition effects (real gfxtransadapter)': 'CGfxTransAdapterTfx',
        'display frame log': 'white-flashes=',
    }
    # (right after a push the boot is still settling: the frame log's first [mon] line comes ~30 s after the home screen)
    for _ in range(12):
        if lab.lines(any_=['white-flashes='], limit=1):
            break
        time.sleep(5)
    alts = {what: (v if isinstance(v, tuple) else (v,)) for what, v in need.items()}
    got = lab.lines(any_=[a for v in alts.values() for a in v], limit=5000)
    for what, v in alts.items():
        if not any(a in l['text'] for l in got for a in v):
            fails.append('marker missing: %s ("%s")' % (what, '" or "'.join(v)))

    # panics: the kernel's thread-death report (thread=..., exit type 2 = panic). A dead window-server thread left the
    # screen black on 2026-10-28 while every marker above was present.
    deaths = lab.lines(any_=["thread='", 'exit type=', 'category='], limit=5000)
    thread = None
    for l in deaths:
        t = l['text']
        mm = re.search(r"thread='([^']*)'", t)
        if mm:
            thread = mm.group(1)
        elif 'exit type=0x00000002' in t and thread:
            fails.append('panic: %s' % thread)
            thread = None

    # 3. the scenario, watched by the frame log
    if not a.quick:
        m = lab.mark()
        steps = [('key', dict(key='menu')), ('sleep', 2),
                 ('swipe', dict(x1=180, y1=520, x2=180, y2=140, ms=250)), ('sleep', 1),
                 ('swipe', dict(x1=180, y1=140, x2=180, y2=520, ms=250)), ('sleep', 1),
                 ('swipe', dict(x1=180, y1=520, x2=180, y2=140, ms=120)), ('sleep', 1),
                 ('key', dict(key='rsk')), ('sleep', 2),
                 ('key', dict(key='power')), ('sleep', 4),           # short power press: the screen off
                 ('touch', dict(x=210, y=420)), ('sleep', 2),        # a tap on the dark screen
                 ('key', dict(key='power')), ('sleep', 4)]           # the screen on
        for kind, arg in steps:
            if kind == 'sleep':
                time.sleep(arg)
            else:
                r = lab.op(kind, **arg)
                if r.get('error'):
                    fails.append('scenario: %s %s failed: %s' % (kind, arg, r['error']))
        time.sleep(6)                                               # the next [mon] line
        # (the kernel prints every hex value on a line of its own: a record = its header line + the value lines after it)
        frame = lab.lines(after=m, any_=['[EKERN][frame]', 'frames=', ' white%=', ' black%='], limit=1000)
        counts, woke, rec = [], 0, None
        for l in frame:
            t = l['text']
            if 'WHITE frame' in t:
                fails.append('white frame: %s' % t.strip())
            if 'first frame after screen-on' in t:
                woke += 1
                rec = {}
                continue
            mm = re.search(r' (white|black)%=(0x[0-9a-fA-F]+)', t)
            if mm and rec is not None:
                rec[mm.group(1)] = int(mm.group(2), 16)
                if len(rec) == 2:
                    if rec['white'] >= 85:
                        fails.append('the screen came on showing a white frame (white %d%%)' % rec['white'])
                    if rec['black'] >= 98:
                        fails.append('the screen came on BLACK (black %d%%): the UI is not drawing' % rec['black'])
                    rec = None
                continue
            mm = re.search(r'frames=(0x[0-9a-fA-F]+)', t)
            if mm:
                counts.append(int(mm.group(1), 16))
        # the frame log must have SEEN the scenario, or the checks above prove nothing. Classic has no transition effects:
        # menu in, menu out, screen off and on change the picture a handful of times (8 on boot 1925); Alfred animates them
        rend = lab.lines(any_=['UI renderer key 4'], limit=5)
        classic = any('CLASSIC render stages' in l['text'] for l in rend)
        print('  renderer: %s' % ('Classic' if classic else 'Alfred'))
        seen = counts[-1] - counts[0] if len(counts) >= 2 else 0
        if seen < (5 if classic else 10):
            fails.append('frame log saw only %d frames during the scenario (not sampling what the panel shows?)' % seen)
        if not woke:
            fails.append('no "first frame after screen-on" line: the wake was not observed')
        print('  scenario: %d frames sampled, %d wake(s), %d frame-log lines' % (seen, woke, len(frame)))

    for f in fails:
        print('  FAIL ' + f)
    print('phone_gate: %s' % ('PASS' if not fails else 'FAIL (%d)' % len(fails)))
    return 0 if not fails else 1


if __name__ == '__main__':
    sys.exit(main())
