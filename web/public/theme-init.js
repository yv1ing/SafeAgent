(function () {
  try {
    var choice = localStorage.getItem('safeagent-theme');
    var dark =
      choice === 'dark' ||
      (choice !== 'light' && window.matchMedia('(prefers-color-scheme: dark)').matches);
    document.documentElement.dataset.theme = dark ? 'dark' : 'light';
  } catch (error) {
    document.documentElement.dataset.theme = 'light';
  }
})();
