# Harbor Phase 4 design system

Status: IMPLEMENTED. Frontend typecheck PASS. Browser visual and accessibility acceptance recorded separately in `PHASE4_FINAL_REPORT.md`; CSS compilation alone does not establish visual acceptance.

## Identity and hierarchy

Harbor retains its evergreen and stone palette, original H mark, system typography and file-first workspace. Flat surfaces, restrained borders and compact chrome replace decorative cards or large dashboard sections. No external fonts, stock assets, animation library or new UI dependency added.

System font stack: `-apple-system`, `BlinkMacSystemFont`, `Segoe UI`, sans-serif. File names 13px, body 13px, supporting text 11–12px, file-manager title 28px desktop / 25–26px mobile. Login illustration retains original inline SVG and a Georgia headline; no copyrighted artwork imported.

Spacing follows a 4px scale, with 6px gaps where icon/text alignment requires it. Corners reflect hierarchy: 4px indicators, 7px controls, 9px file cards/menus, 12px dialogs. Shadows apply to elevated menus, dialogs, notifications and transfer panel. Transitions last 100–200ms and never delay API actions. Reduced-motion preference reduces animation and transition duration to near zero.

## Semantic palette

| Token | Light | Dark | Purpose |
| --- | --- | --- | --- |
| `--bg` | `#fcfcfa` | `#171d19` | File workspace |
| `--sidebar` | `#f3f5f0` | `#1c241e` | Navigation, media stage |
| `--surface` | `#ffffff` | `#222b24` | Controls and elevated surfaces |
| `--subtle` | `#eef1eb` | `#263029` | Hover/secondary surfaces |
| `--text` | `#26372d` | `#e9f0e8` | Primary text |
| `--muted` | `#657165` | `#a5b2a5` | Supporting text |
| `--secondary` | `#4d5c50` | `#c1cdc0` | Secondary controls |
| `--border` | `#e1e6dc` | `#344136` | Dividers |
| `--border-strong` | `#cdd6c8` | `#48594b` | Surface boundaries |
| `--control-border` | `#879482` | `#718475` | Input/checkbox boundaries |
| `--accent` | `#245b48` | `#acd3b2` | Selection, primary action, focus |
| `--accent-soft` | `#e6efe5` | `#2b4232` | Selection background |
| `--on-accent` | `#ffffff` | `#183b23` | Primary-action label |
| `--danger` | `#ac372e` | `#f4afa4` | Destructive state |
| `--danger-soft` | `#fff0ec` | `#442d29` | Destructive background |
| `--folder` | `#9b8a50` | `#d0bc7b` | Folder icon |
| `--folder-soft` | `#f0ead7` | `#3c392b` | Folder-card tint |

Login inherits active theme. Historical forced-light login colors removed. File-kind icons retain semantic color plus shape/text, so color is not sole identifier.

## Workspace geometry

- Expanded navigation: 224px, 208px at laptop widths below 1200px. Desktop rail: 76px, with visible expand control. Mobile drawer remains full navigation regardless of saved desktop collapse.
- Persistent topbar: 64px. Content inset: 32px desktop, 24px laptop, 12–16px mobile. Framed search uses same focus language as form inputs.
- File rows: 56px desktop, 68px at widths ≤700px. Header 40px / 44px. These values match TanStack Virtual estimates.
- File cards: 172px, with 108px visual and 64px metadata region. Virtual row pitch: 184px including 12px gap. Desktop minimum card width 200px; mobile uses two columns. Folder cards use warm stone tint; optional thumbnails load only after explicit enablement.
- Mobile icon/action controls: 44px. Inputs use 16px text to avoid focus zoom. Checkboxes retain 16px visual indicator inside 32px desktop / 44px mobile button target.
- Details inspector becomes overlay below 1100px. Previews occupy full mobile viewport; content scrolls inside stage. Transfer panel stays within viewport and scrolls internally.

## Shared components

Radix supplies dialog/menu focus management and collision handling. Menu portals use 12px viewport collision padding. IconButton defaults to `type="button"`, preventing accidental form submission. Owner login exposes explicit label, invalid state and associated error text; password visibility control disables during authentication.

Selection, drag destination, cut state and busy state have distinct visual feedback. Inline rename has real Save/Cancel controls. Mobile list editing spans remaining row columns; grid editing overlays a compact two-row form inside unchanged card bounds, preserving 44px Save/Cancel targets and virtual geometry. Clipboard banner states current operation. Command palette shares dialog geometry, active option colors and compact keyboard hints. Image controls, PDF page controls and transfer state groups follow same control scale.

Native media controls, bounded text previews and encrypted streaming remain unchanged in purpose. Styling does not create unsupported upload/download actions. See UX report for exact implementation limits.

## Contrast checks

Computed using WCAG relative luminance against exact semantic token values, without opacity or backdrop assumptions:

| Pair | Light | Dark |
| --- | ---: | ---: |
| Primary text / workspace | 12.27:1 | 14.76:1 |
| Muted text / workspace | 4.98:1 | 7.76:1 |
| Muted text / surface | 5.12:1 | 6.61:1 |
| Secondary text / subtle | 6.22:1 | 8.30:1 |
| Accent / selected surface | 6.70:1 | 6.59:1 |
| Primary-action label / accent | 7.88:1 | 7.53:1 |
| Danger / danger surface | 5.67:1 | 6.97:1 |
| Input boundary / workspace | 3.10:1 | 4.29:1 |

All listed text pairs exceed 4.5:1; listed control boundaries exceed 3:1. These token checks complement browser/axe checks, and do not claim every possible rendered state is certified.

## Verification boundary

Agent source verification: `npm --prefix web run typecheck` PASS after integration; `git diff --check` PASS for design-owned files. Coordinator owns running production build, real authenticated screenshots, desktop/mobile interaction tests, axe checks and final visual acceptance. Screenshots under `docs/screenshots/` contain generated fixtures only.
