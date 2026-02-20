import { useEffect, useRef, useCallback } from 'react';

/** Thresholds for detecting a left-edge swipe gesture. */
export interface EdgeSwipeConfig {
  /** Maximum distance (px) from the left viewport edge for swipe start. */
  edgeWidth: number;
  /** Minimum horizontal distance (px) the finger must travel to trigger. */
  minimumDistance: number;
  /** Maximum vertical drift (px) allowed before the gesture is cancelled. */
  maximumVerticalDrift: number;
}

export const DEFAULT_EDGE_SWIPE_CONFIG: EdgeSwipeConfig = {
  edgeWidth: 24,
  minimumDistance: 60,
  maximumVerticalDrift: 50,
};

/**
 * Hook that detects a right-swipe starting near the left edge of the viewport.
 *
 * Attaches passive touch listeners to the given element (or document).
 * Only activates when the `enabled` flag is true, so desktop viewports
 * pay no runtime cost. Uses pointer-capture-style tracking without
 * calling preventDefault(), so normal scrolling in the main content area
 * is unaffected unless a horizontal swipe clearly originates at the edge.
 */
export function useEdgeSwipe(
  onSwipe: () => void,
  enabled: boolean,
  config: EdgeSwipeConfig = DEFAULT_EDGE_SWIPE_CONFIG,
  targetRef?: React.RefObject<HTMLElement | null>,
): void {
  const onSwipeRef = useRef(onSwipe);
  onSwipeRef.current = onSwipe;

  const configRef = useRef(config);
  configRef.current = config;

  const trackingRef = useRef<{
    startX: number;
    startY: number;
    triggered: boolean;
  } | null>(null);

  const handleTouchStart = useCallback((event: TouchEvent) => {
    const touch = event.touches[0];
    if (!touch) return;
    const { edgeWidth } = configRef.current;
    if (touch.clientX <= edgeWidth) {
      trackingRef.current = {
        startX: touch.clientX,
        startY: touch.clientY,
        triggered: false,
      };
    }
  }, []);

  const handleTouchMove = useCallback((event: TouchEvent) => {
    const tracking = trackingRef.current;
    if (!tracking || tracking.triggered) return;

    const touch = event.touches[0];
    if (!touch) return;

    const { minimumDistance, maximumVerticalDrift } = configRef.current;
    const deltaX = touch.clientX - tracking.startX;
    const deltaY = Math.abs(touch.clientY - tracking.startY);

    // Cancel if the user drifts too far vertically (they're scrolling).
    if (deltaY > maximumVerticalDrift) {
      trackingRef.current = null;
      return;
    }

    if (deltaX >= minimumDistance) {
      tracking.triggered = true;
      onSwipeRef.current();
    }
  }, []);

  const handleTouchEnd = useCallback(() => {
    trackingRef.current = null;
  }, []);

  useEffect(() => {
    if (!enabled) return;

    const element = targetRef?.current ?? document;
    // Passive listeners so we don't block the browser's scroll handling.
    const options: AddEventListenerOptions = { passive: true };

    element.addEventListener('touchstart', handleTouchStart as EventListener, options);
    element.addEventListener('touchmove', handleTouchMove as EventListener, options);
    element.addEventListener('touchend', handleTouchEnd as EventListener, options);
    element.addEventListener('touchcancel', handleTouchEnd as EventListener, options);

    return () => {
      element.removeEventListener('touchstart', handleTouchStart as EventListener);
      element.removeEventListener('touchmove', handleTouchMove as EventListener);
      element.removeEventListener('touchend', handleTouchEnd as EventListener);
      element.removeEventListener('touchcancel', handleTouchEnd as EventListener);
    };
  }, [enabled, targetRef, handleTouchStart, handleTouchMove, handleTouchEnd]);
}
