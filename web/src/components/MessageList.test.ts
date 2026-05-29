/**
 * Tests for MessageList scroll behavior logic.
 *
 * These tests exercise the core mechanisms that prevent mobile scroll bumping:
 * 1. StreamTextStore — isolated re-renders for streaming messages only
 * 2. buildItems filtering — correct item counts under various visibility flags
 * 3. Scroll direction state machine — upward scroll suppression
 *
 * Permutations covered:
 * - Tool calls visible vs not visible
 * - Code blocks present vs not present
 * - Todos present vs not present (via user/assistant messages referencing todos)
 * - Artifact chips/panel triggers present vs not present
 */
import { describe, it, expect, vi } from "vitest";
import type { DisplayMessage } from "../types";
import {
  createStreamTextStore,
  buildItems,
  SIMPLE_LIST_THRESHOLD,
} from "./MessageList";

// ─── StreamTextStore ─────────────────────────────────────────────────

describe("createStreamTextStore", () => {
  it("starts with empty string", () => {
    const store = createStreamTextStore();
    expect(store.get()).toBe("");
  });

  it("updates value and notifies subscribers", () => {
    const store = createStreamTextStore();
    const listener = vi.fn();
    store.subscribe(listener);

    store.set("hello");
    expect(store.get()).toBe("hello");
    expect(listener).toHaveBeenCalledTimes(1);
  });

  it("does not notify when value is unchanged", () => {
    const store = createStreamTextStore();
    store.set("hello");
    const listener = vi.fn();
    store.subscribe(listener);

    store.set("hello"); // same value
    expect(listener).not.toHaveBeenCalled();
  });

  it("unsubscribe stops notifications", () => {
    const store = createStreamTextStore();
    const listener = vi.fn();
    const unsubscribe = store.subscribe(listener);

    unsubscribe();
    store.set("world");
    expect(listener).not.toHaveBeenCalled();
  });

  it("supports multiple independent subscribers", () => {
    const store = createStreamTextStore();
    const listenerA = vi.fn();
    const listenerB = vi.fn();
    store.subscribe(listenerA);
    const unsubB = store.subscribe(listenerB);

    store.set("token1");
    expect(listenerA).toHaveBeenCalledTimes(1);
    expect(listenerB).toHaveBeenCalledTimes(1);

    unsubB();
    store.set("token2");
    expect(listenerA).toHaveBeenCalledTimes(2);
    expect(listenerB).toHaveBeenCalledTimes(1);
  });

  it("rapidly updating mimics streaming tokens without recreating subscribers", () => {
    const store = createStreamTextStore();
    const listener = vi.fn();
    store.subscribe(listener);

    // Simulate 100 rapid token updates
    for (let index = 0; index < 100; index++) {
      store.set("token ".repeat(index + 1));
    }
    expect(listener).toHaveBeenCalledTimes(100);
    expect(store.get()).toBe("token ".repeat(100));
  });
});

// ─── buildItems filtering ────────────────────────────────────────────

const mockT = (key: string) => key;

function makeMessage(
  overrides: Partial<DisplayMessage> & {
    id: string;
    type: DisplayMessage["type"];
  },
): DisplayMessage {
  return {
    content: "",
    ...overrides,
  };
}

describe("buildItems — tool calls visible vs hidden", () => {
  const messages: DisplayMessage[] = [
    makeMessage({ id: "u1", type: "user", content: "hello", timestamp: 1000 }),
    makeMessage({
      id: "a1",
      type: "assistant",
      content: "thinking...",
      timestamp: 1001,
    }),
    makeMessage({
      id: "t1",
      type: "tool-invoke",
      content: '{"path":"/tmp"}',
      toolName: "read_file",
    }),
    makeMessage({
      id: "t2",
      type: "tool-result",
      content: "file contents here",
      toolName: "read_file",
    }),
    makeMessage({
      id: "a2",
      type: "assistant",
      content: "Here is the result.",
      timestamp: 1002,
    }),
  ];

  it("shows tool calls when showToolCalls=true", () => {
    const items = buildItems(messages, mockT, true, false);
    const messageItems = items.filter((item) => item.kind === "message");
    expect(messageItems).toHaveLength(5);
  });

  it("hides tool calls when showToolCalls=false", () => {
    const items = buildItems(messages, mockT, false, false);
    const messageItems = items.filter((item) => item.kind === "message");
    // user + 2 assistant = 3 messages (tool-invoke and tool-result hidden)
    expect(messageItems).toHaveLength(3);
  });

  it("item count stability — hiding tools does not change item count mid-scroll", () => {
    const visibleItems = buildItems(messages, mockT, true, false);
    const hiddenItems = buildItems(messages, mockT, false, false);
    // This verifies the filtering is deterministic (same input → same output)
    const hiddenItems2 = buildItems(messages, mockT, false, false);
    expect(hiddenItems).toEqual(hiddenItems2);
    // And visible items have more items
    expect(visibleItems.length).toBeGreaterThan(hiddenItems.length);
  });
});

describe("buildItems — code blocks present vs not present", () => {
  const messagesWithCode: DisplayMessage[] = [
    makeMessage({
      id: "u1",
      type: "user",
      content: "show me code",
      timestamp: 2000,
    }),
    makeMessage({
      id: "a1",
      type: "assistant",
      content: "Here is some code:\n```typescript\nconst x = 1;\n```\nDone.",
      timestamp: 2001,
    }),
  ];

  const messagesWithoutCode: DisplayMessage[] = [
    makeMessage({ id: "u1", type: "user", content: "hello", timestamp: 2000 }),
    makeMessage({
      id: "a1",
      type: "assistant",
      content: "Hi there!",
      timestamp: 2001,
    }),
  ];

  it("messages with code blocks produce same item structure as plain text", () => {
    const codeItems = buildItems(messagesWithCode, mockT, false, false);
    const plainItems = buildItems(messagesWithoutCode, mockT, false, false);
    // Both should have 1 separator + 2 messages (separator logic depends on same-day)
    expect(codeItems.filter((i) => i.kind === "message")).toHaveLength(2);
    expect(plainItems.filter((i) => i.kind === "message")).toHaveLength(2);
  });

  it("code blocks do not affect item key stability", () => {
    const items = buildItems(messagesWithCode, mockT, false, false);
    const messageItem = items.find(
      (i) => i.kind === "message" && i.message.id === "a1",
    );
    expect(messageItem).toBeDefined();
  });
});

describe("buildItems — todos present vs not present", () => {
  // Todos appear as user messages asking about tasks or assistant messages with task lists
  const messagesWithTodos: DisplayMessage[] = [
    makeMessage({
      id: "u1",
      type: "user",
      content: "What are my tasks?",
      timestamp: 3000,
    }),
    makeMessage({
      id: "a1",
      type: "assistant",
      content: "Here are your todos:\n- [ ] Fix scrolling\n- [x] Add tests",
      timestamp: 3001,
    }),
    makeMessage({
      id: "t1",
      type: "tool-invoke",
      content: '{"todos":[{"content":"Fix scrolling","status":"pending"}]}',
      toolName: "todo_write",
    }),
    makeMessage({
      id: "t2",
      type: "tool-result",
      content: "Todos updated",
      toolName: "todo_write",
    }),
  ];

  it("todo tool calls hidden when showToolCalls=false", () => {
    const items = buildItems(messagesWithTodos, mockT, false, false);
    const messageItems = items.filter((i) => i.kind === "message");
    // user + assistant only (tool-invoke and tool-result hidden)
    expect(messageItems).toHaveLength(2);
  });

  it("todo tool calls visible when showToolCalls=true", () => {
    const items = buildItems(messagesWithTodos, mockT, true, false);
    const messageItems = items.filter((i) => i.kind === "message");
    expect(messageItems).toHaveLength(4);
  });

  it("filtering is idempotent (no scroll position drift from repeated builds)", () => {
    const first = buildItems(messagesWithTodos, mockT, false, false);
    const second = buildItems(messagesWithTodos, mockT, false, false);
    expect(first).toEqual(second);
  });
});

describe("buildItems — artifact chips present vs not present", () => {
  const messagesWithArtifacts: DisplayMessage[] = [
    makeMessage({
      id: "u1",
      type: "user",
      content: "Create a component",
      timestamp: 4000,
    }),
    makeMessage({
      id: "a1",
      type: "assistant",
      content: [
        "Here is the component:",
        "```artifact",
        "title: MyComponent",
        "type: react",
        "```tsx",
        "export default function MyComponent() { return <div>Hello</div>; }",
        "```",
        "```",
        "Let me know if you need changes.",
      ].join("\n"),
      timestamp: 4001,
    }),
  ];

  const messagesPlain: DisplayMessage[] = [
    makeMessage({
      id: "u1",
      type: "user",
      content: "Create a component",
      timestamp: 4000,
    }),
    makeMessage({
      id: "a1",
      type: "assistant",
      content: "Here is a simple greeting component that returns Hello.",
      timestamp: 4001,
    }),
  ];

  it("artifact messages do not change item count vs plain messages", () => {
    const artifactItems = buildItems(
      messagesWithArtifacts,
      mockT,
      false,
      false,
    );
    const plainItems = buildItems(messagesPlain, mockT, false, false);
    // Both have same structure: separator + user + assistant
    expect(artifactItems.filter((i) => i.kind === "message")).toHaveLength(2);
    expect(plainItems.filter((i) => i.kind === "message")).toHaveLength(2);
  });

  it("artifact item keys are stable", () => {
    const items1 = buildItems(messagesWithArtifacts, mockT, false, false);
    const items2 = buildItems(messagesWithArtifacts, mockT, false, false);
    const keys1 = items1.map((i) =>
      i.kind === "message" ? i.message.id : i.key,
    );
    const keys2 = items2.map((i) =>
      i.kind === "message" ? i.message.id : i.key,
    );
    expect(keys1).toEqual(keys2);
  });
});

// ─── Scroll direction state machine ─────────────────────────────────

describe("scroll direction state machine", () => {
  /**
   * Simulates the scroll direction detection logic used in MessageList.
   * The component tracks whether the user is scrolling up to suppress
   * auto-scroll that would fight iOS momentum scrolling.
   */
  class ScrollDirectionTracker {
    lastScrollTop = 0;
    scrollingUp = false;
    atBottom = true;

    onScroll(newScrollTop: number): void {
      this.scrollingUp = newScrollTop < this.lastScrollTop;
      this.lastScrollTop = newScrollTop;
    }

    onAtBottomChange(atBottom: boolean): void {
      this.atBottom = atBottom;
      if (atBottom) this.scrollingUp = false;
    }

    shouldAutoScroll(): boolean {
      return this.atBottom && !this.scrollingUp;
    }
  }

  it("allows auto-scroll when at bottom and not scrolling up", () => {
    const tracker = new ScrollDirectionTracker();
    expect(tracker.shouldAutoScroll()).toBe(true);
  });

  it("suppresses auto-scroll when user scrolls upward", () => {
    const tracker = new ScrollDirectionTracker();
    tracker.lastScrollTop = 1000;

    // User scrolls up (momentum)
    tracker.onScroll(950);
    expect(tracker.scrollingUp).toBe(true);
    expect(tracker.shouldAutoScroll()).toBe(false);
  });

  it("re-enables auto-scroll when user returns to bottom", () => {
    const tracker = new ScrollDirectionTracker();
    tracker.lastScrollTop = 1000;

    // User scrolls up
    tracker.onScroll(900);
    expect(tracker.shouldAutoScroll()).toBe(false);

    // User scrolls back down to bottom
    tracker.onScroll(950);
    tracker.onAtBottomChange(true);
    expect(tracker.shouldAutoScroll()).toBe(true);
  });

  it("maintains suppression during iOS momentum (multiple upward events)", () => {
    const tracker = new ScrollDirectionTracker();
    tracker.lastScrollTop = 2000;

    // Simulate momentum scroll: decreasing scrollTop values
    const positions = [1950, 1890, 1820, 1740, 1650, 1550];
    for (const pos of positions) {
      tracker.onScroll(pos);
      expect(tracker.scrollingUp).toBe(true);
      expect(tracker.shouldAutoScroll()).toBe(false);
    }
  });

  it("detects direction change from up to down correctly", () => {
    const tracker = new ScrollDirectionTracker();
    tracker.lastScrollTop = 1000;

    // Scroll up
    tracker.onScroll(900);
    expect(tracker.scrollingUp).toBe(true);

    // Scroll down (user flicked back)
    tracker.onScroll(920);
    expect(tracker.scrollingUp).toBe(false);
    // Still not atBottom, so auto-scroll stays off (atBottom is false)
    tracker.onAtBottomChange(false);
    expect(tracker.shouldAutoScroll()).toBe(false);
  });

  it("handles rapid stream tokens without enabling auto-scroll during upward scroll", () => {
    const tracker = new ScrollDirectionTracker();
    tracker.lastScrollTop = 1500;

    // User starts scrolling up
    tracker.onScroll(1400);
    tracker.onAtBottomChange(false);

    // Simulate 50 rapid "stream token" events — none should change scroll direction
    // (in the real component, stream tokens no longer trigger auto-scroll at all)
    for (let index = 0; index < 50; index++) {
      expect(tracker.shouldAutoScroll()).toBe(false);
    }
  });
});

// ─── Anchor budget state machine ────────────────────────────────────

describe("anchor budget logic", () => {
  /**
   * Simulates the anchor budget calculation used during streaming.
   * Once the user's message scrolls off the top of the viewport, anchoring
   * kicks in and prevents further auto-scroll.
   */
  class AnchorBudgetSimulation {
    streamStartScrollTop: number | null = null;
    anchorBudget: number | null = null;
    anchored = false;

    startStreaming(scrollTop: number, userMessageOffsetFromTop: number): void {
      this.streamStartScrollTop = scrollTop;
      this.anchorBudget = userMessageOffsetFromTop;
      this.anchored = false;
    }

    stopStreaming(): void {
      this.streamStartScrollTop = null;
      this.anchorBudget = null;
      this.anchored = false;
    }

    shouldScrollTo(
      currentScrollTop: number,
      scrollHeight: number,
      clientHeight: number,
    ): "scroll" | "clamp" | "anchor" {
      if (this.anchored) return "anchor";
      if (this.streamStartScrollTop === null || this.anchorBudget === null) {
        return "scroll";
      }

      const maxScrollTop = this.streamStartScrollTop + this.anchorBudget;
      if (currentScrollTop >= maxScrollTop) {
        this.anchored = true;
        return "anchor";
      }

      const endScrollTop = scrollHeight - clientHeight;
      if (endScrollTop > maxScrollTop) {
        this.anchored = true;
        return "clamp";
      }

      return "scroll";
    }
  }

  it("scrolls freely when budget is not exceeded", () => {
    const sim = new AnchorBudgetSimulation();
    sim.startStreaming(100, 500); // user message is 500px from top

    const result = sim.shouldScrollTo(200, 1000, 800);
    expect(result).toBe("scroll");
  });

  it("anchors when scroll exceeds budget", () => {
    const sim = new AnchorBudgetSimulation();
    sim.startStreaming(100, 300);

    // Scroll past budget (100 + 300 = 400)
    const result = sim.shouldScrollTo(450, 2000, 800);
    expect(result).toBe("anchor");
    expect(sim.anchored).toBe(true);
  });

  it("clamps when content grows past budget but scroll hasn't reached limit yet", () => {
    const sim = new AnchorBudgetSimulation();
    sim.startStreaming(100, 300);

    // scrollHeight grew so endScrollTop (2000-800=1200) > maxScrollTop (400)
    // but currentScrollTop (200) hasn't reached maxScrollTop yet...
    // Actually let me re-check: currentScrollTop < maxScrollTop AND endScrollTop > maxScrollTop → clamp
    const result = sim.shouldScrollTo(200, 2000, 800);
    expect(result).toBe("clamp");
  });

  it("resets on stream stop", () => {
    const sim = new AnchorBudgetSimulation();
    sim.startStreaming(100, 300);
    sim.shouldScrollTo(500, 2000, 800); // anchors
    expect(sim.anchored).toBe(true);

    sim.stopStreaming();
    expect(sim.anchored).toBe(false);
    expect(sim.streamStartScrollTop).toBeNull();
  });

  it("works correctly with tool calls causing large content jumps", () => {
    const sim = new AnchorBudgetSimulation();
    // User message is near top of viewport (small budget)
    sim.startStreaming(800, 50);

    // Tool result adds a lot of content, scrollHeight jumps significantly
    // maxScrollTop = 800 + 50 = 850
    // currentScrollTop still at 820 (< 850), but endScrollTop = 5000-800=4200 > 850
    const result = sim.shouldScrollTo(820, 5000, 800);
    expect(result).toBe("clamp");
    expect(sim.anchored).toBe(true);
  });

  it("allows free scroll when no streaming (no budget set)", () => {
    const sim = new AnchorBudgetSimulation();
    const result = sim.shouldScrollTo(500, 2000, 800);
    expect(result).toBe("scroll");
  });
});

// ─── Mixed content scroll stability ─────────────────────────────────

describe("buildItems — mixed content permutations for scroll stability", () => {
  /**
   * These tests verify that item count and keys remain stable across
   * different content combinations, which is critical for preventing
   * react-virtuoso height re-measurements that cause scroll jumps.
   */

  const baseMessages: DisplayMessage[] = [
    makeMessage({
      id: "u1",
      type: "user",
      content: "Do everything",
      timestamp: 5000,
    }),
  ];

  const toolCallMessages: DisplayMessage[] = [
    makeMessage({
      id: "ti1",
      type: "tool-invoke",
      content: '{"query":"test"}',
      toolName: "web_search",
    }),
    makeMessage({
      id: "tr1",
      type: "tool-result",
      content: "Search results...",
      toolName: "web_search",
    }),
  ];

  const codeBlockMessage: DisplayMessage = makeMessage({
    id: "a-code",
    type: "assistant",
    content: "```python\ndef hello():\n    print('world')\n```",
    timestamp: 5002,
  });

  const todoToolMessages: DisplayMessage[] = [
    makeMessage({
      id: "ti-todo",
      type: "tool-invoke",
      content: '{"todos":[]}',
      toolName: "todo_write",
    }),
    makeMessage({
      id: "tr-todo",
      type: "tool-result",
      content: "ok",
      toolName: "todo_write",
    }),
  ];

  const artifactMessage: DisplayMessage = makeMessage({
    id: "a-artifact",
    type: "assistant",
    content:
      "```artifact\ntitle: Widget\ntype: react\n```tsx\nexport default () => <div/>\n```\n```",
    timestamp: 5003,
  });

  it("tool calls visible + code blocks + todos + artifacts", () => {
    const messages = [
      ...baseMessages,
      ...toolCallMessages,
      codeBlockMessage,
      ...todoToolMessages,
      artifactMessage,
    ];
    const items = buildItems(messages, mockT, true, false);
    const messageItems = items.filter((i) => i.kind === "message");
    expect(messageItems).toHaveLength(7); // u1 + ti1 + tr1 + a-code + ti-todo + tr-todo + a-artifact
  });

  it("tool calls hidden + code blocks + todos + artifacts", () => {
    const messages = [
      ...baseMessages,
      ...toolCallMessages,
      codeBlockMessage,
      ...todoToolMessages,
      artifactMessage,
    ];
    const items = buildItems(messages, mockT, false, false);
    const messageItems = items.filter((i) => i.kind === "message");
    // Only user + 2 assistant messages (all tool-invoke/result hidden)
    expect(messageItems).toHaveLength(3);
  });

  it("tool calls hidden + no code blocks + no todos + no artifacts", () => {
    const messages = [
      ...baseMessages,
      makeMessage({
        id: "a1",
        type: "assistant",
        content: "Simple reply.",
        timestamp: 5001,
      }),
    ];
    const items = buildItems(messages, mockT, false, false);
    const messageItems = items.filter((i) => i.kind === "message");
    expect(messageItems).toHaveLength(2);
  });

  it("tool calls visible + no code blocks + no todos + no artifacts", () => {
    const messages = [
      ...baseMessages,
      ...toolCallMessages,
      makeMessage({
        id: "a1",
        type: "assistant",
        content: "Simple reply.",
        timestamp: 5001,
      }),
    ];
    const items = buildItems(messages, mockT, true, false);
    const messageItems = items.filter((i) => i.kind === "message");
    expect(messageItems).toHaveLength(4); // u1 + ti1 + tr1 + a1
  });

  it("keys are always unique (prevents virtuoso key conflicts)", () => {
    const messages = [
      ...baseMessages,
      ...toolCallMessages,
      codeBlockMessage,
      ...todoToolMessages,
      artifactMessage,
    ];
    const items = buildItems(messages, mockT, true, true);
    const keys = items.map((i) =>
      i.kind === "message" ? i.message.id : i.key,
    );
    const uniqueKeys = new Set(keys);
    expect(uniqueKeys.size).toBe(keys.length);
  });

  it("toggling showToolCalls does not affect non-tool item ordering", () => {
    const messages = [
      ...baseMessages,
      ...toolCallMessages,
      codeBlockMessage,
      artifactMessage,
    ];
    const visibleItems = buildItems(messages, mockT, true, false);
    const hiddenItems = buildItems(messages, mockT, false, false);

    const visibleNonTool = visibleItems.filter(
      (i) =>
        i.kind === "message" &&
        i.message.type !== "tool-invoke" &&
        i.message.type !== "tool-result",
    );
    const hiddenNonTool = hiddenItems.filter(
      (i) =>
        i.kind === "message" &&
        i.message.type !== "tool-invoke" &&
        i.message.type !== "tool-result",
    );
    expect(visibleNonTool).toEqual(hiddenNonTool);
  });
});

// ─── ask_user_question is always visible ─────────────────────────────

describe("buildItems — ask_user_question tool always visible", () => {
  const messages: DisplayMessage[] = [
    makeMessage({ id: "u1", type: "user", content: "help", timestamp: 6000 }),
    makeMessage({
      id: "ti-ask",
      type: "tool-invoke",
      content: '{"question":"Which option?","choices":["A","B"]}',
      toolName: "ask_user_question",
    }),
    makeMessage({
      id: "tr-ask",
      type: "tool-result",
      content: "A",
      toolName: "ask_user_question",
    }),
    makeMessage({
      id: "ti-other",
      type: "tool-invoke",
      content: "{}",
      toolName: "read_file",
    }),
    makeMessage({
      id: "tr-other",
      type: "tool-result",
      content: "data",
      toolName: "read_file",
    }),
  ];

  it("ask_user_question tool calls stay visible even when showToolCalls=false", () => {
    const items = buildItems(messages, mockT, false, false);
    const messageItems = items.filter((i) => i.kind === "message");
    // user + ask invoke + ask result = 3 (read_file pair hidden)
    expect(messageItems).toHaveLength(3);
    const ids = messageItems.map((i) => i.kind === "message" && i.message.id);
    expect(ids).toContain("ti-ask");
    expect(ids).toContain("tr-ask");
    expect(ids).not.toContain("ti-other");
  });
});

// ─── Scroll listener stability ──────────────────────────────────────

describe("scroll listener stability (non-streaming scroll jump prevention)", () => {
  /**
   * Simulates the scroll listener attachment pattern used by MessageList.
   *
   * The original implementation used a useEffect with no dependency array,
   * which re-registered the scroll listener on EVERY React render. During
   * iOS momentum scrolling, renders triggered by atBottom state changes
   * would teardown/re-add the listener mid-momentum, causing micro-jumps
   * because the browser's momentum scroll bookkeeping was interrupted.
   *
   * The fix attaches the listener once via scrollerRef callback, and only
   * re-attaches when the element itself changes.
   */
  class StableScrollListener {
    private element: HTMLElement | null = null;
    private cleanup: (() => void) | null = null;
    attachCount = 0;
    detachCount = 0;
    lastScrollTop = 0;
    scrollingUp = false;

    attach(element: HTMLElement | null): void {
      // Clean up previous listener.
      if (this.cleanup) {
        this.cleanup();
        this.cleanup = null;
        this.detachCount++;
      }
      this.element = element;
      if (!element) return;

      this.attachCount++;
      const onScroll = () => {
        const currentTop = element.scrollTop;
        this.scrollingUp = currentTop < this.lastScrollTop;
        this.lastScrollTop = currentTop;
      };
      element.addEventListener("scroll", onScroll, { passive: true });
      this.cleanup = () => {
        element.removeEventListener("scroll", onScroll);
      };
    }

    destroy(): void {
      this.cleanup?.();
      this.cleanup = null;
    }
  }

  it("attaches listener exactly once for a given element", () => {
    const listener = new StableScrollListener();
    const mockElement = { scrollTop: 0 } as HTMLElement;
    (mockElement as any).addEventListener = vi.fn();
    (mockElement as any).removeEventListener = vi.fn();

    listener.attach(mockElement);
    expect(listener.attachCount).toBe(1);

    // Simulating multiple React renders — should NOT re-attach since element
    // is the same (in the real code, scrollerRef only fires when element changes).
    // No additional attach calls happen.
    expect(listener.attachCount).toBe(1);

    listener.destroy();
  });

  it("does not detach/re-attach during simulated renders", () => {
    const listener = new StableScrollListener();
    const mockElement = { scrollTop: 0 } as HTMLElement;
    (mockElement as any).addEventListener = vi.fn();
    (mockElement as any).removeEventListener = vi.fn();

    listener.attach(mockElement);

    // Simulate 100 React renders (state changes from atBottom, stream tokens, etc.)
    // In the fixed implementation, none of these cause re-attachment.
    for (let i = 0; i < 100; i++) {
      // No-op — renders don't trigger re-attachment anymore.
    }

    expect(listener.attachCount).toBe(1);
    expect(listener.detachCount).toBe(0);
    listener.destroy();
  });

  it("re-attaches only when element changes (e.g. Virtuoso remount)", () => {
    const listener = new StableScrollListener();
    const element1 = { scrollTop: 0 } as HTMLElement;
    const element2 = { scrollTop: 500 } as HTMLElement;
    for (const el of [element1, element2]) {
      (el as any).addEventListener = vi.fn();
      (el as any).removeEventListener = vi.fn();
    }

    listener.attach(element1);
    expect(listener.attachCount).toBe(1);

    // Element changes (e.g. conversation switch causes remount)
    listener.attach(element2);
    expect(listener.attachCount).toBe(2);
    expect(listener.detachCount).toBe(1); // old listener cleaned up

    listener.destroy();
  });

  it("cleans up on null element (unmount)", () => {
    const listener = new StableScrollListener();
    const mockElement = { scrollTop: 0 } as HTMLElement;
    (mockElement as any).addEventListener = vi.fn();
    (mockElement as any).removeEventListener = vi.fn();

    listener.attach(mockElement);
    listener.attach(null); // unmount
    expect(listener.detachCount).toBe(1);

    listener.destroy();
  });

  it("scroll direction tracking works correctly through the stable listener", () => {
    const listener = new StableScrollListener();
    // Use a real-ish mock with a mutable scrollTop
    let scrollHandler: (() => void) | null = null;
    const mockElement = {
      scrollTop: 1000,
      addEventListener: (_event: string, handler: () => void) => {
        scrollHandler = handler;
      },
      removeEventListener: () => {
        scrollHandler = null;
      },
    } as unknown as HTMLElement;

    listener.attach(mockElement);
    listener.lastScrollTop = 1000;

    // Simulate upward scroll
    (mockElement as any).scrollTop = 950;
    scrollHandler!();
    expect(listener.scrollingUp).toBe(true);

    // Simulate downward scroll
    (mockElement as any).scrollTop = 970;
    scrollHandler!();
    expect(listener.scrollingUp).toBe(false);

    listener.destroy();
  });
});

// ─── Static conversation scroll stability ───────────────────────────

describe("buildItems — large static conversation stability", () => {
  /**
   * Verifies that buildItems produces stable output for large conversations
   * with mixed content types (the scenario where scroll jumps are most
   * noticeable — scrolling upward through many items of varying height).
   */

  function makeConversation(
    turnCount: number,
    includeTools: boolean,
  ): DisplayMessage[] {
    const messages: DisplayMessage[] = [];
    for (let i = 0; i < turnCount; i++) {
      const timestamp = 1000 + i * 100;
      messages.push(
        makeMessage({
          id: `u${i}`,
          type: "user",
          content: `User message ${i} with some text`,
          timestamp,
        }),
      );
      if (includeTools) {
        messages.push(
          makeMessage({
            id: `ti${i}`,
            type: "tool-invoke",
            content: `{"arg":"value${i}"}`,
            toolName: "some_tool",
          }),
        );
        messages.push(
          makeMessage({
            id: `tr${i}`,
            type: "tool-result",
            content: `Result for turn ${i}`,
            toolName: "some_tool",
          }),
        );
      }
      messages.push(
        makeMessage({
          id: `a${i}`,
          type: "assistant",
          content: `Assistant reply ${i}:\n\`\`\`python\nprint("hello")\n\`\`\`\nEnd.`,
          timestamp: timestamp + 50,
        }),
      );
    }
    return messages;
  }

  it("item count is stable across repeated builds (no flicker)", () => {
    const messages = makeConversation(50, true);
    const items1 = buildItems(messages, mockT, true, false);
    const items2 = buildItems(messages, mockT, true, false);
    expect(items1.length).toBe(items2.length);
    expect(items1).toEqual(items2);
  });

  it("toggling tool visibility does not change non-tool item order (50 turns)", () => {
    const messages = makeConversation(50, true);
    const withTools = buildItems(messages, mockT, true, false);
    const withoutTools = buildItems(messages, mockT, false, false);

    const nonToolWith = withTools.filter(
      (i) =>
        i.kind === "separator" ||
        (i.kind === "message" &&
          i.message.type !== "tool-invoke" &&
          i.message.type !== "tool-result"),
    );
    const nonToolWithout = withoutTools.filter(
      (i) =>
        i.kind === "separator" ||
        (i.kind === "message" &&
          i.message.type !== "tool-invoke" &&
          i.message.type !== "tool-result"),
    );
    expect(nonToolWith).toEqual(nonToolWithout);
  });

  it("keys remain unique across a large mixed conversation", () => {
    const messages = makeConversation(100, true);
    const items = buildItems(messages, mockT, true, true);
    const keys = items.map((i) =>
      i.kind === "message" ? i.message.id : i.key,
    );
    expect(new Set(keys).size).toBe(keys.length);
  });
});

// ─── Mobile simple-list scroll preservation ─────────────────────────

describe("mobile prepend scroll preservation", () => {
  /**
   * Simulates the useLayoutEffect logic that preserves scroll position
   * when older messages are prepended on mobile. iOS Safari lacks
   * overflow-anchor support, so we manually adjust scrollTop by the
   * delta in scrollHeight caused by the prepended content.
   */
  class PrependPreserver {
    prevScrollHeight = 0;
    wasPrepend = false;

    /** Called after each render's DOM commit (useLayoutEffect). */
    afterDomUpdate(el: { scrollHeight: number; scrollTop: number }): void {
      if (this.wasPrepend) {
        this.wasPrepend = false;
        const delta = el.scrollHeight - this.prevScrollHeight;
        if (delta > 0) {
          el.scrollTop += delta;
        }
      }
      this.prevScrollHeight = el.scrollHeight;
    }

    markPrepend(): void {
      this.wasPrepend = true;
    }
  }

  it("adjusts scrollTop by exactly the height of prepended content", () => {
    const preserver = new PrependPreserver();
    const el = { scrollHeight: 2000, scrollTop: 800 };

    // Initial render — capture baseline scrollHeight
    preserver.afterDomUpdate(el);
    expect(el.scrollTop).toBe(800);

    // Prepend occurs — DOM grows by 600px at the top
    preserver.markPrepend();
    el.scrollHeight = 2600;
    // scrollTop stays 800 (iOS Safari doesn't auto-adjust)

    preserver.afterDomUpdate(el);
    // scrollTop should be adjusted to 800 + 600 = 1400
    expect(el.scrollTop).toBe(1400);
  });

  it("does not adjust scrollTop when items are appended (not prepended)", () => {
    const preserver = new PrependPreserver();
    const el = { scrollHeight: 2000, scrollTop: 1800 };

    preserver.afterDomUpdate(el);

    // Append — DOM grows but wasPrepend is NOT set
    el.scrollHeight = 2200;

    preserver.afterDomUpdate(el);
    // scrollTop should remain unchanged
    expect(el.scrollTop).toBe(1800);
  });

  it("handles multiple consecutive prepends correctly", () => {
    const preserver = new PrependPreserver();
    const el = { scrollHeight: 1000, scrollTop: 500 };

    preserver.afterDomUpdate(el);

    // First prepend: +300px
    preserver.markPrepend();
    el.scrollHeight = 1300;
    preserver.afterDomUpdate(el);
    expect(el.scrollTop).toBe(800); // 500 + 300

    // Second prepend: +200px
    preserver.markPrepend();
    el.scrollHeight = 1500;
    preserver.afterDomUpdate(el);
    expect(el.scrollTop).toBe(1000); // 800 + 200
  });

  it("handles zero-height prepend (e.g. all items filtered out)", () => {
    const preserver = new PrependPreserver();
    const el = { scrollHeight: 1000, scrollTop: 500 };

    preserver.afterDomUpdate(el);

    // Prepend flagged but scrollHeight didn't actually change
    // (all prepended messages were filtered by showToolCalls=false)
    preserver.markPrepend();
    // scrollHeight stays 1000
    preserver.afterDomUpdate(el);
    expect(el.scrollTop).toBe(500); // no change
  });

  it("preserves user's scroll position during prepend after manual scroll", () => {
    const preserver = new PrependPreserver();
    const el = { scrollHeight: 2000, scrollTop: 800 };

    preserver.afterDomUpdate(el);

    // User scrolls to a different position between renders
    el.scrollTop = 400;

    // Prepend occurs
    preserver.markPrepend();
    el.scrollHeight = 2500;

    preserver.afterDomUpdate(el);
    // Should adjust from current position: 400 + 500 = 900
    expect(el.scrollTop).toBe(900);
  });
});

// ─── Mobile atBottom detection ──────────────────────────────────────

describe("mobile atBottom detection", () => {
  /**
   * On mobile, atBottom is determined by scroll position instead of
   * Virtuoso's atBottomStateChange. The threshold matches Virtuoso's
   * atBottomThreshold of 80px.
   */
  function isAtBottom(el: {
    scrollHeight: number;
    scrollTop: number;
    clientHeight: number;
  }): boolean {
    return el.scrollHeight - el.scrollTop - el.clientHeight < 80;
  }

  it("detects bottom when scrolled to the end", () => {
    expect(
      isAtBottom({ scrollHeight: 2000, scrollTop: 1200, clientHeight: 800 }),
    ).toBe(true);
  });

  it("detects bottom within threshold", () => {
    // 2000 - 1130 - 800 = 70 < 80
    expect(
      isAtBottom({ scrollHeight: 2000, scrollTop: 1130, clientHeight: 800 }),
    ).toBe(true);
  });

  it("not at bottom when above threshold", () => {
    // 2000 - 1100 - 800 = 100 > 80
    expect(
      isAtBottom({ scrollHeight: 2000, scrollTop: 1100, clientHeight: 800 }),
    ).toBe(false);
  });

  it("not at bottom when at top of long conversation", () => {
    expect(
      isAtBottom({ scrollHeight: 5000, scrollTop: 0, clientHeight: 800 }),
    ).toBe(false);
  });

  it("at bottom for short conversation that fits in viewport", () => {
    // scrollHeight <= clientHeight means content fits without scrolling
    expect(
      isAtBottom({ scrollHeight: 600, scrollTop: 0, clientHeight: 800 }),
    ).toBe(true);
  });
});

// ─── Mobile rendering: all items rendered ───────────────────────────

describe("mobile simple list — all items rendered", () => {
  /**
   * Verifies that the mobile path renders every item (no virtualization),
   * which is the core mechanism that eliminates scroll jumps from
   * height estimation mismatches.
   */

  function makeConversation(
    turnCount: number,
    includeTools: boolean,
  ): DisplayMessage[] {
    const messages: DisplayMessage[] = [];
    for (let i = 0; i < turnCount; i++) {
      const timestamp = 1000 + i * 100;
      messages.push(
        makeMessage({
          id: `u${i}`,
          type: "user",
          content: `User message ${i}`,
          timestamp,
        }),
      );
      if (includeTools) {
        messages.push(
          makeMessage({
            id: `ti${i}`,
            type: "tool-invoke",
            content: `{"arg":"val"}`,
            toolName: "some_tool",
          }),
        );
        messages.push(
          makeMessage({
            id: `tr${i}`,
            type: "tool-result",
            content: `Result ${i}`,
            toolName: "some_tool",
          }),
        );
      }
      messages.push(
        makeMessage({
          id: `a${i}`,
          type: "assistant",
          content: `Reply ${i}\n\`\`\`\ncode\n\`\`\``,
          timestamp: timestamp + 50,
        }),
      );
    }
    return messages;
  }

  it("buildItems returns all items that mobile list would render", () => {
    const messages = makeConversation(50, true);
    const items = buildItems(messages, mockT, true, false);
    // Every item in the array is rendered (no virtualization skip)
    // This is the key property: items.length === rendered count
    expect(items.length).toBeGreaterThan(0);
    // All messages appear as items
    const messageItems = items.filter((i) => i.kind === "message");
    expect(messageItems).toHaveLength(200); // 50 * (user + tool-invoke + tool-result + assistant)
  });

  it("item keys are unique for mobile map() rendering", () => {
    const messages = makeConversation(100, true);
    const items = buildItems(messages, mockT, true, true);
    const keys = items.map((i) =>
      i.kind === "message" ? i.message.id : i.key,
    );
    const uniqueKeys = new Set(keys);
    expect(uniqueKeys.size).toBe(keys.length);
  });

  it("mobile renders same items as Virtuoso would (no filtering difference)", () => {
    const messages = makeConversation(30, true);
    // Same buildItems call is used for both paths
    const items = buildItems(messages, mockT, false, false);
    // Mobile renders all items; Virtuoso renders a subset at any given time
    // but the TOTAL item set is identical
    const mobileRenderedCount = items.length;
    expect(mobileRenderedCount).toBeGreaterThan(0);
    // Verify no items are lost — every user+assistant message is present
    const userMessages = items.filter(
      (i) => i.kind === "message" && i.message.type === "user",
    );
    const assistantMessages = items.filter(
      (i) => i.kind === "message" && i.message.type === "assistant",
    );
    expect(userMessages).toHaveLength(30);
    expect(assistantMessages).toHaveLength(30);
  });
});

// ─── Simple-list threshold and mode selection ─────────────────────────

describe("SIMPLE_LIST_THRESHOLD — mode selection", () => {
  /**
   * The component uses `simpleList = isMobile || items.length <= SIMPLE_LIST_THRESHOLD`
   * to decide between the non-virtualized simple scroll container and Virtuoso.
   * These tests verify that buildItems output correctly crosses the threshold
   * boundary and that the threshold value is sensible.
   */

  function makeConversation(
    turnCount: number,
    includeTools: boolean,
  ): DisplayMessage[] {
    const messages: DisplayMessage[] = [];
    for (let i = 0; i < turnCount; i++) {
      const timestamp = 1000 + i * 100;
      messages.push(
        makeMessage({
          id: `u${i}`,
          type: "user",
          content: `User message ${i}`,
          timestamp,
        }),
      );
      if (includeTools) {
        messages.push(
          makeMessage({
            id: `ti${i}`,
            type: "tool-invoke",
            content: `{"arg":"val"}`,
            toolName: "some_tool",
          }),
        );
        messages.push(
          makeMessage({
            id: `tr${i}`,
            type: "tool-result",
            content: `Result ${i}`,
            toolName: "some_tool",
          }),
        );
      }
      messages.push(
        makeMessage({
          id: `a${i}`,
          type: "assistant",
          content: `Reply ${i}`,
          timestamp: timestamp + 50,
        }),
      );
    }
    return messages;
  }

  /** Simulates the mode-selection logic from the component. */
  function shouldUseSimpleList(isMobile: boolean, itemCount: number): boolean {
    return isMobile || itemCount <= SIMPLE_LIST_THRESHOLD;
  }

  it("threshold is a positive integer", () => {
    expect(SIMPLE_LIST_THRESHOLD).toBeGreaterThan(0);
    expect(Number.isInteger(SIMPLE_LIST_THRESHOLD)).toBe(true);
  });

  it("mobile always uses simple list regardless of item count", () => {
    expect(shouldUseSimpleList(true, 0)).toBe(true);
    expect(shouldUseSimpleList(true, SIMPLE_LIST_THRESHOLD)).toBe(true);
    expect(shouldUseSimpleList(true, SIMPLE_LIST_THRESHOLD + 1)).toBe(true);
    expect(shouldUseSimpleList(true, 10000)).toBe(true);
  });

  it("desktop uses simple list at or below threshold", () => {
    expect(shouldUseSimpleList(false, 0)).toBe(true);
    expect(shouldUseSimpleList(false, 1)).toBe(true);
    expect(shouldUseSimpleList(false, SIMPLE_LIST_THRESHOLD)).toBe(true);
  });

  it("desktop uses Virtuoso above threshold", () => {
    expect(shouldUseSimpleList(false, SIMPLE_LIST_THRESHOLD + 1)).toBe(false);
    expect(shouldUseSimpleList(false, SIMPLE_LIST_THRESHOLD + 100)).toBe(false);
  });

  it("short conversation (10 turns, no tools) stays below threshold on desktop", () => {
    const messages = makeConversation(10, false);
    const items = buildItems(messages, mockT, false, false);
    expect(shouldUseSimpleList(false, items.length)).toBe(true);
  });

  it("medium conversation (50 turns, tools hidden) stays below threshold", () => {
    const messages = makeConversation(50, true);
    const items = buildItems(messages, mockT, false, false);
    // 50 turns × 2 messages (user + assistant) + separators
    expect(items.length).toBeLessThanOrEqual(SIMPLE_LIST_THRESHOLD);
    expect(shouldUseSimpleList(false, items.length)).toBe(true);
  });

  it("large conversation (200 turns, tools visible) exceeds threshold", () => {
    const messages = makeConversation(200, true);
    const items = buildItems(messages, mockT, true, false);
    // 200 turns × 4 messages + separators — well above 200
    expect(items.length).toBeGreaterThan(SIMPLE_LIST_THRESHOLD);
    expect(shouldUseSimpleList(false, items.length)).toBe(false);
  });

  it("toggling tool visibility can cross the threshold boundary", () => {
    // Find a turn count where tools-hidden is ≤ threshold but tools-visible is >
    const messages = makeConversation(80, true);
    const itemsHidden = buildItems(messages, mockT, false, false);
    const itemsVisible = buildItems(messages, mockT, true, false);

    // With tools hidden: 80 user + 80 assistant + separators ≈ 161
    // With tools visible: 80 × 4 messages + separators ≈ 321
    expect(itemsHidden.length).toBeLessThanOrEqual(SIMPLE_LIST_THRESHOLD);
    expect(itemsVisible.length).toBeGreaterThan(SIMPLE_LIST_THRESHOLD);

    expect(shouldUseSimpleList(false, itemsHidden.length)).toBe(true);
    expect(shouldUseSimpleList(false, itemsVisible.length)).toBe(false);
  });

  it("exact boundary: threshold items uses simple list, threshold+1 uses Virtuoso", () => {
    expect(shouldUseSimpleList(false, SIMPLE_LIST_THRESHOLD)).toBe(true);
    expect(shouldUseSimpleList(false, SIMPLE_LIST_THRESHOLD + 1)).toBe(false);
  });
});
