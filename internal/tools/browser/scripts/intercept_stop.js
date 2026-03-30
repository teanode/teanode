(() => {
  if (window.__teanodeNetIntercept) {
    window.__teanodeNetIntercept.disconnect();
    delete window.__teanodeNetIntercept;
  }
  const captures = window.__teanodeNetCaptures || [];
  delete window.__teanodeNetCaptures;
  return captures.length;
})()
