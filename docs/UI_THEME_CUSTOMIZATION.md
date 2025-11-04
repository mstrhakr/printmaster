# UI Theme Customization Guide

This guide explains how to customize the PrintMaster Agent UI colors and styling through centralized CSS variables.

## Location

All theme variables are defined in `agent/main.go` in the `<style>` section under `:root`.

## Color Variables

### Base Theme Colors

```css
--bg: #002b36;              /* Main background color */
--panel: #073642;           /* Panel/card background */
--muted: #586e75;           /* Muted text color */
--text: #93a1a1;            /* Main text color */
--accent: #b58900;          /* Accent color (headers, highlights) */
--highlight: #268bd2;       /* Interactive element highlight (links, focus) */
--border: #004b56;          /* Default border color */
```

### Toggle Switch Colors

```css
/* OFF state */
--toggle-off-bg-start: #003842;          /* Gradient start */
--toggle-off-bg-end: #004b56;            /* Gradient end */
--toggle-off-border: #005b66;            /* Border color */

/* ON state */
--toggle-on-bg-start: #2aa198;           /* Gradient start */
--toggle-on-bg-end: #268bd2;             /* Gradient end */
--toggle-on-border: #2aa198;             /* Border color */
--toggle-on-glow: rgba(38, 139, 210, 0.4); /* Glow effect */

/* Toggle knob (OFF) */
--toggle-knob-off-start: #657b83;        /* Gradient start */
--toggle-knob-off-end: #586e75;          /* Gradient end */

/* Toggle knob (ON) */
--toggle-knob-on-start: #ffffff;         /* Gradient start */
--toggle-knob-on-end: #f0f0f0;           /* Gradient end */

/* Indeterminate state (section toggles) */
--toggle-indeterminate-start: #cb4b16;   /* Gradient start */
--toggle-indeterminate-end: #b58900;     /* Gradient end */
--toggle-indeterminate-glow: rgba(181, 137, 0, 0.3); /* Glow effect */
```

### Button Colors

```css
/* Primary buttons */
--btn-primary-bg: #268bd2;               /* Background */
--btn-primary-text: #002b36;             /* Text color */

/* Save buttons */
--btn-save-bg: #2d7a3e;                  /* Background */
--btn-save-border: #3a9d50;              /* Border (also hover color) */
--btn-save-text: #d0f0d8;                /* Text color */

/* Saved state buttons */
--btn-saved-bg: #2d5a3e;                 /* Background */
--btn-saved-border: #3a5d50;             /* Border */
--btn-saved-text: #90b098;               /* Text color */

/* Delete buttons */
--btn-delete-bg: #992626;                /* Background */
--btn-delete-border: #c63535;            /* Border */
--btn-delete-text: #ffdddd;              /* Text color */
```

### Shadow System

Pre-defined shadow levels for consistent depth:

```css
--shadow-sm: 0 1px 2px rgba(0, 0, 0, 0.2);           /* Small shadow */
--shadow-md: 0 2px 4px rgba(0, 0, 0, 0.3);           /* Medium shadow */
--shadow-lg: 0 3px 6px rgba(0, 0, 0, 0.4);           /* Large shadow */
--shadow-xl: 0 4px 8px rgba(0, 0, 0, 0.5);           /* Extra large shadow */
--shadow-inset-sm: inset 0 1px 3px rgba(0, 0, 0, 0.4);  /* Small inset */
--shadow-inset-md: inset 0 2px 4px rgba(0, 0, 0, 0.5);  /* Medium inset */
```

### Transition Timing

Consistent animation speeds:

```css
--transition-fast: 0.15s ease;                       /* Quick transitions */
--transition-normal: 0.2s ease;                      /* Normal transitions */
--transition-smooth: 0.3s cubic-bezier(0.4, 0, 0.2, 1); /* Smooth easing */
```

## Quick Theme Examples

### Light Theme

To convert to a light theme, change these core variables:

```css
:root {
  --bg: #fdf6e3;              /* Light cream background */
  --panel: #eee8d5;           /* Slightly darker panel */
  --muted: #93a1a1;           /* Gray text */
  --text: #657b83;            /* Dark gray text */
  --accent: #b58900;          /* Keep accent */
  --highlight: #268bd2;       /* Keep highlight */
  --border: #d3cbb8;          /* Light border */
}
```

### High Contrast Dark

For better accessibility:

```css
:root {
  --bg: #000000;              /* Pure black */
  --panel: #1a1a1a;           /* Very dark gray */
  --text: #ffffff;            /* Pure white text */
  --accent: #ffaa00;          /* Bright orange */
  --highlight: #00aaff;       /* Bright blue */
  --border: #333333;          /* Dark gray border */
}
```

### Custom Brand Colors

Replace toggle switch colors with your brand:

```css
:root {
  /* Example: Purple/Pink brand */
  --toggle-on-bg-start: #9b59b6;
  --toggle-on-bg-end: #e91e63;
  --toggle-on-border: #9b59b6;
  --toggle-on-glow: rgba(155, 89, 182, 0.5);
  
  --btn-primary-bg: #9b59b6;
  --highlight: #9b59b6;
}
```

## Elements Using These Variables

- **Buttons**: All button types use `--btn-*` variables
- **Toggle Switches**: Desktop (44×24px) and mobile (52×28px) use `--toggle-*` variables
- **Inputs**: Text, number, select elements use `--bg`, `--border`, `--highlight`
- **Panels**: Cards and containers use `--panel` and `--border`
- **Tabs**: Active tabs use `--highlight` and `--panel`
- **Details/Summary**: Collapsible sections use `--border` and `--highlight`
- **Shadows**: All interactive elements use `--shadow-*` levels
- **Transitions**: Hover/active states use `--transition-*` timing

## Mobile Considerations

- Mobile toggles are automatically larger (52×28px vs 44×24px)
- Shadow depths increase on mobile for better visibility
- All variables apply consistently across screen sizes
- Touch targets meet WCAG 2.1 accessibility guidelines (minimum 44×44px)

## Tips for Theme Changes

1. **Test Contrast**: Ensure text contrast meets WCAG AA standards (4.5:1 for normal text)
2. **Shadow Consistency**: Use the predefined shadow levels for visual hierarchy
3. **Hover States**: Button hover uses `filter: brightness(1.08)` automatically
4. **Focus States**: Inputs get a 2px glow using `--highlight` color
5. **Glow Effects**: Toggle switches and scan indicators use colored glows when active

## Future Enhancements

Potential additions to the theme system:

- [ ] Multiple theme presets selectable in UI
- [ ] Theme persistence in agent.db
- [ ] Color picker in settings for custom themes
- [ ] Export/import theme configurations
- [ ] Real-time theme preview
