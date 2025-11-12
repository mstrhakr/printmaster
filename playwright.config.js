// Minimal Playwright config for the smoke test
module.exports = {
  timeout: 30000,
  use: {
    headless: true,
  },
  projects: [
    { name: 'chromium', use: { browserName: 'chromium' } }
  ]
};
