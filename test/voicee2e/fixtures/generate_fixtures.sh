#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

OS="$(uname -s)"
tts_backend=""

if [[ "$OS" == "Darwin" ]]; then
  if ! command -v say >/dev/null 2>&1; then
    echo "error: 'say' command is required on macOS" >&2
    exit 1
  fi
  if ! command -v afconvert >/dev/null 2>&1; then
    echo "error: 'afconvert' command is required on macOS" >&2
    exit 1
  fi
  tts_backend="macos"
else
  if command -v espeak >/dev/null 2>&1 && command -v ffmpeg >/dev/null 2>&1; then
    tts_backend="linux"
  else
    echo "error: on Linux, both 'espeak' and 'ffmpeg' are required" >&2
    exit 1
  fi
fi

tmp="$(mktemp -d)"
cleanup() { rm -rf "$tmp"; }
trap cleanup EXIT

gen() {
  local out="$1"
  local text="$2"
  local out_path="$ROOT_DIR/$out"
  if [[ "$tts_backend" == "macos" ]]; then
    local aiff="$tmp/${out%.wav}.aiff"
    say -o "$aiff" "$text"
    afconvert "$aiff" "$out_path" -f WAVE -d LEI16@16000 -c 1 >/dev/null
  else
    # Generate deterministic mono PCM16/16k WAV for fixture consumption.
    local wav_tmp="$tmp/${out%.wav}.wav"
    espeak -w "$wav_tmp" "$text"
    ffmpeg -loglevel error -y -i "$wav_tmp" -ac 1 -ar 16000 -sample_fmt s16 "$out_path"
  fi
  echo "generated $ROOT_DIR/$out"
}

gen "short_hello.wav" "Hello, can you summarize what we changed?"
gen "medium_question.wav" "I have two follow up questions about the voice pipeline and testing."
gen "long_explanation.wav" "Let me explain the issue in detail and then ask for a concrete next step to improve quality."
gen "mt_turn1.wav" "First question, what changed in the latest patch?"
gen "mt_turn2.wav" "Second question, how do I test this end to end?"
gen "interrupt_q1.wav" "Explain what this service does."
gen "interrupt_followup.wav" "Stop and tell me only the top risk."
gen "rapid_seed.wav" "Summarize the current status."
gen "rapid_interrupt_1.wav" "No, focus on latency only."
gen "rapid_interrupt_2.wav" "Now give one concrete action."

echo "done"
