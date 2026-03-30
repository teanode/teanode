(() => {
  const navigationEntries = performance.getEntriesByType('navigation');
  return {
    url: location.href,
    readyState: document.readyState,
    timeOrigin: performance.timeOrigin || 0,
    navigationCount: navigationEntries.length,
  };
})()
