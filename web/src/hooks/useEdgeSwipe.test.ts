/**
 * Tests for the edge-swipe gesture detection logic used by useEdgeSwipe.
 *
 * Since the hook is DOM-coupled (touch events + React refs), these tests
 * exercise the pure state-machine logic by simulating the same touch
 * sequence decisions the hook makes internally.
 */
import { describe, it, expect } from "vitest";
import {
  DEFAULT_EDGE_SWIPE_CONFIG,
  type EdgeSwipeConfig,
} from "./useEdgeSwipe";

// ── Simulate the gesture state machine extracted from useEdgeSwipe ──

interface TouchPoint {
  clientX: number;
  clientY: number;
}

class EdgeSwipeSimulation {
  config: EdgeSwipeConfig;
  swipeCount = 0;
  private tracking: {
    startX: number;
    startY: number;
    triggered: boolean;
  } | null = null;

  constructor(config: EdgeSwipeConfig = DEFAULT_EDGE_SWIPE_CONFIG) {
    this.config = config;
  }

  touchStart(point: TouchPoint): void {
    if (point.clientX <= this.config.edgeWidth) {
      this.tracking = {
        startX: point.clientX,
        startY: point.clientY,
        triggered: false,
      };
    }
  }

  touchMove(point: TouchPoint): void {
    if (!this.tracking || this.tracking.triggered) return;

    const deltaX = point.clientX - this.tracking.startX;
    const deltaY = Math.abs(point.clientY - this.tracking.startY);

    if (deltaY > this.config.maximumVerticalDrift) {
      this.tracking = null;
      return;
    }

    if (deltaX >= this.config.minimumDistance) {
      this.tracking.triggered = true;
      this.swipeCount++;
    }
  }

  touchEnd(): void {
    this.tracking = null;
  }
}

// ── Tests ──────────────────────────────────────────────────────────

describe("edge swipe gesture detection", () => {
  describe("default config values", () => {
    it("has sensible defaults", () => {
      expect(DEFAULT_EDGE_SWIPE_CONFIG.edgeWidth).toBe(24);
      expect(DEFAULT_EDGE_SWIPE_CONFIG.minimumDistance).toBe(60);
      expect(DEFAULT_EDGE_SWIPE_CONFIG.maximumVerticalDrift).toBe(50);
    });
  });

  describe("successful swipe detection", () => {
    it("triggers when swiping right from the left edge", () => {
      const simulation = new EdgeSwipeSimulation();
      simulation.touchStart({ clientX: 10, clientY: 200 });
      simulation.touchMove({ clientX: 80, clientY: 200 });

      expect(simulation.swipeCount).toBe(1);
    });

    it("triggers when starting exactly at the edge boundary", () => {
      const simulation = new EdgeSwipeSimulation();
      simulation.touchStart({ clientX: 24, clientY: 100 });
      simulation.touchMove({ clientX: 90, clientY: 100 });

      expect(simulation.swipeCount).toBe(1);
    });

    it("triggers with slight vertical movement within tolerance", () => {
      const simulation = new EdgeSwipeSimulation();
      simulation.touchStart({ clientX: 5, clientY: 300 });
      simulation.touchMove({ clientX: 70, clientY: 340 });

      expect(simulation.swipeCount).toBe(1);
    });

    it("fires only once per gesture even with continued movement", () => {
      const simulation = new EdgeSwipeSimulation();
      simulation.touchStart({ clientX: 10, clientY: 200 });
      simulation.touchMove({ clientX: 80, clientY: 200 });
      simulation.touchMove({ clientX: 150, clientY: 200 });
      simulation.touchMove({ clientX: 200, clientY: 200 });

      expect(simulation.swipeCount).toBe(1);
    });
  });

  describe("rejected gestures", () => {
    it("ignores touches starting outside the edge zone", () => {
      const simulation = new EdgeSwipeSimulation();
      simulation.touchStart({ clientX: 50, clientY: 200 });
      simulation.touchMove({ clientX: 120, clientY: 200 });

      expect(simulation.swipeCount).toBe(0);
    });

    it("cancels when vertical drift exceeds threshold", () => {
      const simulation = new EdgeSwipeSimulation();
      simulation.touchStart({ clientX: 10, clientY: 200 });
      simulation.touchMove({ clientX: 80, clientY: 260 });

      expect(simulation.swipeCount).toBe(0);
    });

    it("does not trigger for insufficient horizontal distance", () => {
      const simulation = new EdgeSwipeSimulation();
      simulation.touchStart({ clientX: 10, clientY: 200 });
      simulation.touchMove({ clientX: 40, clientY: 200 });

      expect(simulation.swipeCount).toBe(0);
    });

    it("resets tracking on touch end", () => {
      const simulation = new EdgeSwipeSimulation();
      simulation.touchStart({ clientX: 10, clientY: 200 });
      simulation.touchEnd();
      // Subsequent move after end should not trigger.
      simulation.touchMove({ clientX: 80, clientY: 200 });

      expect(simulation.swipeCount).toBe(0);
    });

    it("ignores leftward swipes from the edge", () => {
      const simulation = new EdgeSwipeSimulation();
      simulation.touchStart({ clientX: 10, clientY: 200 });
      // Move left — deltaX is negative, never reaches minimumDistance.
      simulation.touchMove({ clientX: -50, clientY: 200 });

      expect(simulation.swipeCount).toBe(0);
    });
  });

  describe("custom config", () => {
    it("respects a wider edge activation zone", () => {
      const simulation = new EdgeSwipeSimulation({
        edgeWidth: 50,
        minimumDistance: 60,
        maximumVerticalDrift: 50,
      });
      simulation.touchStart({ clientX: 45, clientY: 200 });
      simulation.touchMove({ clientX: 110, clientY: 200 });

      expect(simulation.swipeCount).toBe(1);
    });

    it("respects a shorter minimum distance", () => {
      const simulation = new EdgeSwipeSimulation({
        edgeWidth: 24,
        minimumDistance: 30,
        maximumVerticalDrift: 50,
      });
      simulation.touchStart({ clientX: 10, clientY: 200 });
      simulation.touchMove({ clientX: 45, clientY: 200 });

      expect(simulation.swipeCount).toBe(1);
    });

    it("respects a tighter vertical drift limit", () => {
      const simulation = new EdgeSwipeSimulation({
        edgeWidth: 24,
        minimumDistance: 60,
        maximumVerticalDrift: 20,
      });
      simulation.touchStart({ clientX: 10, clientY: 200 });
      simulation.touchMove({ clientX: 80, clientY: 225 });

      expect(simulation.swipeCount).toBe(0);
    });
  });

  describe("multiple gestures", () => {
    it("can detect multiple sequential swipes", () => {
      const simulation = new EdgeSwipeSimulation();

      simulation.touchStart({ clientX: 10, clientY: 200 });
      simulation.touchMove({ clientX: 80, clientY: 200 });
      simulation.touchEnd();

      simulation.touchStart({ clientX: 5, clientY: 300 });
      simulation.touchMove({ clientX: 70, clientY: 300 });
      simulation.touchEnd();

      expect(simulation.swipeCount).toBe(2);
    });
  });
});
