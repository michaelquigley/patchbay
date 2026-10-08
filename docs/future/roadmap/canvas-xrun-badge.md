---
title: canvas xrun badge
state: researching
created: 2026-10-07
tags: [enhancement]
log:
  - stamp: 2026-10-07
    note: badge built after the title, not right-aligned — docs/journal/2026-10-07.md
---

settle the canvas xrun badge's placement and look by eye. it draws `+N xruns` in the warning hue through `Label` after a block's title and hidden suffix (`internal/ui/canvas.go`, `frame.badgeXruns` in `plan.go`). if it should sit flush at the title bar's right edge instead, use the previous frame's node width, since a node is measured from its content in the frame it is drawn; keep it detent-safe and absent when monitoring is unavailable or the view is stale.
