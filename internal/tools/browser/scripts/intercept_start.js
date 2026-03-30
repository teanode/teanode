(() => {
  if (window.__teanodeNetIntercept) {
    window.__teanodeNetIntercept.disconnect();
  }
  window.__teanodeNetCaptures = [];
  const pattern = %s;
  const regex = pattern ? new RegExp(pattern) : null;
  window.__teanodeNetIntercept = new PerformanceObserver((list) => {
    for (const entry of list.getEntries()) {
      if (entry.entryType === 'resource') {
        if (!regex || regex.test(entry.name)) {
          window.__teanodeNetCaptures.push({
            url: entry.name,
            method: entry.initiatorType,
            duration: Math.round(entry.duration),
            transferSize: entry.transferSize || 0,
            status: entry.responseStatus || 0,
          });
        }
      }
    }
  });
  window.__teanodeNetIntercept.observe({type: 'resource', buffered: false});
  return true;
})()
