# Shared Web Assets Architecture

## Overview

PrintMaster uses **go:embed within the common package** to share CSS and JavaScript between the agent and server UIs. Actual `.css` and `.js` files in `common/web/` are embedded there and exported as string variables.

## Structure

```
common/
  web/
    shared.css         # Actual CSS file - Theme colors, modals, buttons, etc.
    shared.js          # Actual JavaScript file - Utilities, formatters, etc.
    shared.go          # Embeds shared.css and shared.js, exports as strings

agent/
  web/
    index.html         # Loads /static/shared.css and /static/shared.js
    app.js             # Agent-specific JavaScript
    style.css          # Agent-specific styles
  main.go              # Imports common/web, serves SharedCSS and SharedJS

server/
  web/
    index.html         # Loads /static/shared.css and /static/shared.js
    app.js             # Server-specific JavaScript  
    style.css          # Server-specific styles
  main.go              # Imports common/web, serves SharedCSS and SharedJS
```

## How It Works

1. **Source Files**: Edit `common/web/shared.css` and `common/web/shared.js` as normal files
2. **Embedding**: `common/web/shared.go` uses `//go:embed` to embed these files into exported strings
3. **Import**: Agent and server import `sharedweb "printmaster/common/web"`
4. **Runtime Serving**: When `/static/shared.css` or `/static/shared.js` requested, serve from `sharedweb.SharedCSS` / `sharedweb.SharedJS`
5. **Compilation**: Go embed happens at compile time - changes require rebuild

## Benefits

✅ **Real source files** - Full editor support (syntax highlighting, linting, autocomplete)  
✅ **No parent path issues** - `go:embed` works within common/web directory  
✅ **Single source** - Edit once in `common/web/`, used by both modules  
✅ **Type-safe** - Go compiler validates files exist at compile time  
✅ **Performance** - Served directly from memory, no file I/O  
✅ **Self-contained** - Each binary embeds its own copy  
✅ **Standard Go pattern** - Idiomatic use of go:embed  

## Editing Shared Assets

### To modify shared CSS:
1. Edit `common/web/shared.css` directly in your editor
2. Rebuild: `.\build.ps1 both` (or agent/server individually)
3. Changes automatically embedded in new binaries

### To modify shared JavaScript:
1. Edit `common/web/shared.js` directly in your editor
2. Rebuild: `.\build.ps1 both`
3. Functions automatically available to both UIs

**No sync scripts, no file copying, no build hooks needed!**

## Module-Specific Styles

Agent and server still have their own `style.css` for component-specific styling:
- Agent: Device table, scanner controls, discovery UI
- Server: Agent management, fleet view, aggregation dashboards

The load order ensures shared styles load first, allowing module overrides:
```html
<link rel="stylesheet" href="/static/shared.css">   <!-- Common base -->
<link rel="stylesheet" href="/static/style.css">     <!-- Module-specific -->
```

## Why Not go:embed with Parent Paths?

Go's `//go:embed` directive doesn't support `../` paths for security/portability. Options considered:

1. ❌ **Root web/ directory**: Breaks module boundaries, requires complex embed paths
2. ❌ **Symlinks**: Platform-dependent, Git complications
3. ❌ **Build-time copying**: Fragile, easy to forget, sync issues
4. ✅ **String constant**: Simple, reliable, Go-native solution

## Implementation Details

### Agent (agent/main.go)
```go
import sharedweb "printmaster/common/web"

http.HandleFunc("/static/", func(w http.ResponseWriter, r *http.Request) {
    fileName := strings.TrimPrefix(r.URL.Path, "/static/")
    
    if fileName == "shared.css" {
        w.Header().Set("Content-Type", "text/css; charset=utf-8")
        w.Write([]byte(sharedweb.SharedCSS))
        return
    }
    
    // ... serve other files from webFS embed ...
})
```

### Server (server/main.go)
```go
import sharedweb "printmaster/common/web"

func handleStatic(w http.ResponseWriter, r *http.Request) {
    fileName := strings.TrimPrefix(r.URL.Path, "/static/")
    
    if fileName == "shared.css" {
        w.Header().Set("Content-Type", "text/css; charset=utf-8")
        w.Write([]byte(sharedweb.SharedCSS))
        return
    }
    
    // ... serve other files from webFS embed ...
}
```

## CSS Organization

### shared.css (common/web/shared.go)
- Theme variables (colors, shadows, transitions)
- Modal system
- Button styles
- Form inputs
- Toast notifications
- Toggle switches
- Utility classes

### agent/web/style.css
- Device grid/table layouts
- Scanner UI components
- Discovery controls
- Agent-specific cards

### server/web/style.css
- Agent fleet view
- Connection status indicators
- Server-specific dashboards
- Multi-agent layouts

## Future Expansion

To add more shared assets (JS, images), extend the pattern:

```go
// common/web/shared.go
package web

const SharedCSS = `...`
const SharedUtilsJS = `...`  // Shared JavaScript utilities
const IconSVG = `...`         // Shared SVG icons
```

Then serve in both modules:
```go
if fileName == "shared.js" {
    w.Write([]byte(sharedweb.SharedUtilsJS))
}
```

This scales cleanly without file system complexity.
