(() => {
  const trackerKey = '__teanodeNetworkIdleTracker';
  const now = () => performance.now();
  if (!window[trackerKey]) {
    const tracker = { activeRequests: 0, lastActivityAt: now(), idleThresholdMs: 500 };
    const markActivity = () => { tracker.lastActivityAt = now(); };
    const beginRequest = () => { tracker.activeRequests += 1; markActivity(); };
    const endRequest = () => { tracker.activeRequests = Math.max(0, tracker.activeRequests - 1); markActivity(); };

    const originalFetch = window.fetch?.bind(window);
    if (originalFetch && !window.__teanodeNetworkIdleFetchWrapped) {
      window.fetch = (...args) => {
        beginRequest();
        return originalFetch(...args).finally(() => endRequest());
      };
      window.__teanodeNetworkIdleFetchWrapped = true;
    }

    const xhrPrototype = window.XMLHttpRequest?.prototype;
    if (xhrPrototype && !xhrPrototype.__teanodeNetworkIdleWrapped) {
      const originalOpen = xhrPrototype.open;
      const originalSend = xhrPrototype.send;
      xhrPrototype.open = function (...args) {
        this.__teanodeNetworkIdleTracked = false;
        return originalOpen.apply(this, args);
      };
      xhrPrototype.send = function (...args) {
        if (!this.__teanodeNetworkIdleTracked) {
          this.__teanodeNetworkIdleTracked = true;
          beginRequest();
          this.addEventListener('loadend', () => {
            if (this.__teanodeNetworkIdleTracked) {
              this.__teanodeNetworkIdleTracked = false;
              endRequest();
            }
          }, { once: true });
        }
        return originalSend.apply(this, args);
      };
      xhrPrototype.__teanodeNetworkIdleWrapped = true;
    }

    window[trackerKey] = tracker;
  }

  return {
    activeRequests: window[trackerKey].activeRequests || 0,
    lastActivityAt: window[trackerKey].lastActivityAt || now(),
    currentTime: now(),
    readyState: document.readyState,
    idleThresholdMs: window[trackerKey].idleThresholdMs || 500,
  };
})()
