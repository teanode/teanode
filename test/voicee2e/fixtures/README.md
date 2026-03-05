# Voice E2E Fixtures

Place 16-bit WAV fixtures here for scenario playback.

Required files from `test/voicee2e/scenarios/suite.yaml`:

- `short_hello.wav`
- `medium_question.wav`
- `long_explanation.wav`
- `mt_turn1.wav`
- `mt_turn2.wav`
- `interrupt_q1.wav`
- `interrupt_followup.wav`
- `rapid_seed.wav`
- `rapid_interrupt_1.wav`
- `rapid_interrupt_2.wav`

Format expectations:

- PCM WAV (`audioFormat=1`)
- 16-bit samples
- mono or stereo (stereo is downmixed)
- any sample rate (resampled to 16k for send path)

Tip for fixture generation on macOS:

```bash
./test/voicee2e/fixtures/generate_fixtures.sh
```

## VAD Silence Invariant

**All WAV fixtures must have max consecutive silence < 320 ms.**

EnergyVAD uses `vadRedemptionFrames = 16` (16 × 20 ms = 320 ms) to detect end-of-speech.
Any silence gap ≥ 320 ms inside a fixture will cause the VAD to split one utterance into
two turns mid-playback, producing a partial transcript and a false barge-in event.

**When editing `generate_fixtures.sh`:** omit commas and mid-sentence punctuation from
TTS input strings. macOS `say` inserts natural pauses (~400–480 ms) at commas, which
exceeds the 320 ms threshold.

**Verify after any fixture change:**

```bash
python3 - <<'EOF'
import wave, struct, math, os, sys
THRESHOLD = 0.02   # RMS below this is silence
FRAME_MS  = 20     # EnergyVAD frame size
LIMIT_MS  = 300    # must be < vadRedemptionFrames × FRAME_MS = 320ms
base = os.path.dirname(os.path.abspath(__file__))
fail = False
for name in sorted(os.listdir(base)):
    if not name.endswith(".wav"): continue
    path = os.path.join(base, name)
    with wave.open(path) as w:
        rate, n = w.getframerate(), w.getnframes()
        raw = w.readframes(n)
    frame_sz = rate * FRAME_MS // 1000
    rms = lambda s: math.sqrt(sum(x*x for x in s)/len(s)) if s else 0
    samples = [struct.unpack_from('<h', raw, i)[0]/32768 for i in range(0,len(raw)-1,2)]
    max_sil, cur_sil = 0, 0
    for i in range(0, len(samples)-frame_sz, frame_sz):
        if rms(samples[i:i+frame_sz]) < THRESHOLD:
            cur_sil += FRAME_MS
            max_sil = max(max_sil, cur_sil)
        else:
            cur_sil = 0
    status = "FAIL" if max_sil >= LIMIT_MS else "ok"
    if status == "FAIL": fail = True
    print(f"{status:4s}  max_silence={max_sil:4d}ms  {name}")
sys.exit(1 if fail else 0)
EOF
```

All fixtures must report `ok` before running the voicee2e suite.
