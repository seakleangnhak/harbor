# Harbor README assets

Original SVG lighthouse branding was created for Harbor. No reference-repository logo, artwork, icon, source or downloaded font was used. The three SVG files contain only vector shapes, local system-font names and accessible descriptions; no scripts, embedded credentials or external resources. They are covered by the root [MIT License](../../LICENSE). Runtime application branding is unchanged.

## Screenshot provenance

Visually reviewed on 2026-10-21. These are actual rendered application captures from generated local-adapter demonstration fixtures, not mockups or a private Telegram installation. Source captures were already part of the Phase 4/5 qualification. See [Phase 4 UX evidence](../PHASE4_UX_REPORT.md), [Phase 5 verification](../PHASE5_FINAL_REPORT.md) and reusable `scripts/phase4_*_check.js` browser checks.

Reviewed content includes generic `Design library` folders, generated Coastline artwork, generated PDF/text/code files and qualification transfer history. Owner labels/avatar are application defaults. No personal document content, phone/email, account/channel identity, invite, authentication code, token, key, credential, session or machine path is visible. The transfer-history numeric suffixes name generated recovery fixtures.

All six screenshots were converted with `cwebp -lossless -m 6 -metadata none`; no cropping, retouching or content replacement. Decoded RGBA SHA-256 matches each original via FFmpeg. GitHub-facing images total **232,468 bytes**, down from **527,780 bytes** of source PNGs, retaining original dimensions and exact pixels.

| Asset | Original capture under `docs/screenshots/` | Dimensions | Bytes |
| --- | --- | --- | ---: |
| [file-manager-light.webp](file-manager-light.webp) | `phase5-desktop-light.png` | 1440 × 900 | 40,512 |
| [file-manager-dark.webp](file-manager-dark.webp) | `phase5-desktop-dark.png` | 1440 × 900 | 45,008 |
| [image-preview.webp](image-preview.webp) | `after-image-preview.png` | 1440 × 900 | 6,656 |
| [pdf-preview.webp](pdf-preview.webp) | `phase5-pdf-preview.png` | 1440 × 900 | 68,916 |
| [transfer-history.webp](transfer-history.webp) | `after-transfers.png` | 1440 × 900 | 54,436 |
| [mobile.webp](mobile.webp) | `phase5-mobile-light.png` | 390 × 844 | 16,940 |

## Asset integrity

SHA-256 of optimized WebP files:

```text
074acda62a1abc60bbe91bfba01b57d99cc800dcd4c173ef126ad3fbf658d741  file-manager-light.webp
d1942c820c7c58c4dd998fa6273f3a8ceddfc8c316dc024f827b5621f972a36b  file-manager-dark.webp
5b2e24e7bca553f96674f14281648f7c4327451a243e15b6fed3536a8a0d3992  image-preview.webp
022cadc95f5e0be13740dca9b2e07334c7f7cddb91175743c183a690ead49d22  pdf-preview.webp
54258b17cd0b74c889874b5b325a3d25abb1b57f07016b7b4c61290f7adeebcb  transfer-history.webp
a272f0a9969b45f53632aa0c6d1c044043a7af3930310eee3a025c4a2634067f  mobile.webp
```

Re-review every new capture before publication. Use an isolated local drive with generated fixtures, capture the real interface, remove image metadata and inspect the full image. Never substitute a live account screenshot without a separate privacy review. Screenshots demonstrate interface behavior; they do not establish Telegram availability or a security guarantee.
