// Playwright config with multiple browsers and viewport sizes
module.exports = {
  testDir: './common/web/__tests__/playwright',
  testMatch: '*.test.js',
  timeout: 30000,
  use: {
    headless: true,
  },
  projects: [
    // ===== CHROMIUM (Chrome/Edge) =====
    // Standard desktop (1080p) - most common
    { 
      name: 'chromium-desktop', 
      use: { 
        browserName: 'chromium',
        viewport: { width: 1920, height: 1080 }
      } 
    },
    // Low-end desktop / laptop (720p)
    { 
      name: 'chromium-desktop-small', 
      use: { 
        browserName: 'chromium',
        viewport: { width: 1280, height: 720 }
      } 
    },
    // Mobile (iPhone 14 size)
    { 
      name: 'chromium-mobile', 
      use: { 
        browserName: 'chromium',
        viewport: { width: 390, height: 844 },
        isMobile: true,
        hasTouch: true
      } 
    },

    // ===== FIREFOX =====
    // Note: Firefox does NOT support isMobile option, so no firefox-mobile project
    { 
      name: 'firefox-desktop', 
      use: { 
        browserName: 'firefox',
        viewport: { width: 1920, height: 1080 }
      } 
    },

    // ===== WEBKIT (Safari) =====
    { 
      name: 'webkit-desktop', 
      use: { 
        browserName: 'webkit',
        viewport: { width: 1920, height: 1080 }
      } 
    },
    { 
      name: 'webkit-mobile', 
      use: { 
        browserName: 'webkit',
        viewport: { width: 390, height: 844 },
        isMobile: true,
        hasTouch: true
      } 
    }
  ]
};
