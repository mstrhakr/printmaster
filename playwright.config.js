// Playwright config with multiple viewport sizes
module.exports = {
  testDir: './common/web/__tests__/playwright',
  testMatch: '*.test.js',
  timeout: 30000,
  use: {
    headless: true,
  },
  projects: [
    // Standard desktop (1080p) - most common
    { 
      name: 'desktop', 
      use: { 
        browserName: 'chromium',
        viewport: { width: 1920, height: 1080 }
      } 
    },
    // Low-end desktop / laptop (720p)
    { 
      name: 'desktop-small', 
      use: { 
        browserName: 'chromium',
        viewport: { width: 1280, height: 720 }
      } 
    },
    // Mobile (iPhone 14 size)
    { 
      name: 'mobile', 
      use: { 
        browserName: 'chromium',
        viewport: { width: 390, height: 844 },
        isMobile: true,
        hasTouch: true
      } 
    }
  ]
};
