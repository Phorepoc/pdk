# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0

"""transcode_tones -- the release's AAC alarm tones as WAV, made on the BUILD HOST.

The 2010 organizer ships two AAC tones (alarmui/DataSrc: clock.aac = Clock's default alarm, alarm.aac = Calendar's)
and names them in code and settings (clkdatetimeview.cpp KHardCodedDigitalAlarmTone, CalendarInternalCRKeys.h). The
clean image carries no AAC decoder, so the build host decodes them once to 16-bit PCM WAV. The ROM keeps the release's
file NAMES (z:\\data\\sounds\\digital\\clock.aac) so no code or setting changes: MMF picks the controller from the
file's header ("RIFF....WAVE" -> the WAV controller), not from the extension.

    python3 tools/transcode_tones.py          -> BUILD/zdata-gen/tones/{clock,alarm}.aac (WAV data)

Needs PyAV (`pip install av`: FFmpeg's decoders, used on the build host only); without it, it says so and the ROM
simply has no tones. zdata_gen.py GENERATED adds the outputs to the ROM when they exist.
"""
import os
import sys
import wave

sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), "emitter"))
from paths import SYMBIAN, BUILD  # noqa: E402

SRC = os.path.join(SYMBIAN, "symbian-rcl3", "oss.FCL.sf.app.organizer", "alarmui", "DataSrc")
OUT = os.path.join(BUILD, "zdata-gen", "tones")
TONES = ("clock.aac", "alarm.aac")


def decode(path: str):
    """-> (rate, channels, interleaved s16le bytes)"""
    import av  # noqa: PLC0415 -- optional build-host dependency
    with av.open(path) as f:
        stream = f.streams.audio[0]
        rate, ch = stream.codec_context.sample_rate, stream.codec_context.channels
        res = av.AudioResampler(format="s16", layout="stereo" if ch > 1 else "mono", rate=rate)
        pcm = bytearray()
        for frame in f.decode(stream):
            for out in res.resample(frame):
                pcm += bytes(out.planes[0])[: out.samples * ch * 2]
        for out in res.resample(None):
            pcm += bytes(out.planes[0])[: out.samples * ch * 2]
    return rate, ch, bytes(pcm)


def main() -> int:
    try:
        import av  # noqa: F401,PLC0415
    except ImportError:
        print("transcode_tones: PyAV not installed (pip install av) -- the ROM gets no alarm tones")
        return 0
    os.makedirs(OUT, exist_ok=True)
    for name in TONES:
        rate, ch, pcm = decode(os.path.join(SRC, name))
        with wave.open(os.path.join(OUT, name), "wb") as w:
            w.setnchannels(ch)
            w.setsampwidth(2)
            w.setframerate(rate)
            w.writeframes(pcm)
        print(f"  {name}: {rate} Hz x{ch}, {len(pcm) / (rate * ch * 2):.2f} s -> {OUT}/{name} (WAV)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
