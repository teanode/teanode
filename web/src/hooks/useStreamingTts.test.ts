/**
 * Tests for the onTurnComplete callback logic in useStreamingTts.
 *
 * Since this hook is tightly coupled to React state, HTMLAudioElement, and
 * fetch(), and the project doesn't have @testing-library/react, these tests
 * exercise the queue-state-machine logic by simulating the internal
 * transitions that determine when onTurnComplete should fire.
 *
 * The core invariants are:
 *   1. onTurnComplete fires when the playback queue drains naturally
 *      (all items reach 'done' via audio 'ended' events).
 *   2. It does NOT fire when stopAndClear() is called (barge-in).
 *   3. It does NOT fire when no audio was actually played (all items errored).
 */
import { describe, it, expect } from "vitest";

// ── Reproduce the queue state machine from useStreamingTts ───────

type ItemStatus = "pending" | "fetching" | "ready" | "playing" | "done";

interface QueueItem {
  text: string;
  audioToken?: string;
  status: ItemStatus;
}

class TTSQueueSimulation {
  queue: QueueItem[] = [];
  playIndex = 0;
  active = false;
  playedAny = false;
  turnCompleteCount = 0;

  enqueue(text: string): void {
    if (!this.active) this.playedAny = false;
    this.active = true;
    this.queue.push({ text, status: "pending" });
  }

  /** Simulate successful fetch for an item. */
  fetchComplete(index: number, token: string): void {
    const item = this.queue[index];
    if (!item || item.status !== "pending") return;
    item.status = "ready";
    item.audioToken = token;
    // If this is the play-head item, start playing.
    if (this.active && this.playIndex === index) {
      this.startPlaying(index);
    }
  }

  /** Simulate fetch error for an item. */
  fetchError(index: number): void {
    const item = this.queue[index];
    if (!item) return;
    item.status = "done";
    this.playIndex++;
    this.tryPlayNext();
  }

  /** Mark item as playing. */
  private startPlaying(index: number): void {
    const item = this.queue[index];
    if (!item || item.status !== "ready") return;
    item.status = "playing";
    this.playedAny = true;
  }

  /** Simulate audio 'ended' event for the current item. */
  audioEnded(): void {
    const item = this.queue[this.playIndex];
    if (item) item.status = "done";
    this.playIndex++;
    this.tryPlayNext();
  }

  /** Try to advance playback — mirrors the playNext() logic. */
  private tryPlayNext(): void {
    if (!this.active) return;
    if (this.playIndex >= this.queue.length) {
      if (this.playedAny) {
        this.turnCompleteCount++;
      }
      return;
    }
    const item = this.queue[this.playIndex];
    if (item.status === "ready") {
      this.startPlaying(this.playIndex);
    }
  }

  /** Simulate stopAndClear (barge-in). */
  stopAndClear(): void {
    this.active = false;
    this.playedAny = false;
    this.queue = [];
    this.playIndex = 0;
  }
}

// ── Tests ────────────────────────────────────────────────────────

describe("useStreamingTts onTurnComplete state machine", () => {
  it("fires onTurnComplete when queue drains after normal playback", () => {
    const simulation = new TTSQueueSimulation();
    simulation.enqueue("Hello.");
    simulation.fetchComplete(0, "tok1");
    simulation.audioEnded();

    expect(simulation.turnCompleteCount).toBe(1);
  });

  it("fires onTurnComplete after multiple items play through", () => {
    const simulation = new TTSQueueSimulation();
    simulation.enqueue("First.");
    simulation.enqueue("Second.");

    simulation.fetchComplete(0, "tok1");
    simulation.audioEnded(); // First done, second not ready yet
    simulation.fetchComplete(1, "tok2");
    // After fetch completes for item at playIndex, it auto-starts.
    simulation.audioEnded(); // Second done

    expect(simulation.turnCompleteCount).toBe(1);
  });

  it("does NOT fire onTurnComplete on stopAndClear (barge-in)", () => {
    const simulation = new TTSQueueSimulation();
    simulation.enqueue("Hello.");
    simulation.fetchComplete(0, "tok1");

    // Barge-in before audio ends.
    simulation.stopAndClear();

    expect(simulation.turnCompleteCount).toBe(0);
  });

  it("does NOT fire onTurnComplete when all items error (no audio played)", () => {
    const simulation = new TTSQueueSimulation();
    simulation.enqueue("Hello.");
    simulation.fetchError(0);

    expect(simulation.turnCompleteCount).toBe(0);
    expect(simulation.playedAny).toBe(false);
  });

  it("fires onTurnComplete even if some items errored but at least one played", () => {
    const simulation = new TTSQueueSimulation();
    simulation.enqueue("First.");
    simulation.enqueue("Second.");

    simulation.fetchComplete(0, "tok1");
    simulation.audioEnded(); // First done
    simulation.fetchError(1); // Second errors

    // turnComplete should fire because at least one item played.
    expect(simulation.turnCompleteCount).toBe(1);
  });

  it("resets playedAny on new turn after stopAndClear", () => {
    const simulation = new TTSQueueSimulation();
    simulation.enqueue("Hello.");
    simulation.fetchComplete(0, "tok1");
    simulation.audioEnded();
    expect(simulation.turnCompleteCount).toBe(1);

    // New turn after barge-in.
    simulation.stopAndClear();
    simulation.enqueue("New turn.");
    simulation.fetchComplete(0, "tok2");
    simulation.audioEnded();

    expect(simulation.turnCompleteCount).toBe(2);
  });
});
