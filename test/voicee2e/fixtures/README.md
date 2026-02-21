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
