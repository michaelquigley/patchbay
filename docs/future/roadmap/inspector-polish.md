---
title: inspector polish
state: researching
created: 2026-10-07
tags: [enhancement]
log:
  - stamp: 2026-10-07
    note: section headers became collapsing bands with a 12px gap — docs/journal/2026-10-07.md
---

work through Michael's next by-eye notes on the inspector, one round per note set: read what he flags in the running window, change only that, keep every fact the inspector shows today, and update `docs/current/ui.md` to match. the layout helpers (`section`, `sectionV`, `beginPairs`, `sectionGap`) live in `internal/ui/inspector.go` and are shared with the performance panel, so a change to them changes both.
