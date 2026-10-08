---
title: show default metadata without subject lifetime tracking
state: inbox
created: 2026-10-07
tags: [enhancement]
---

present default metadata as the raw diagnostic table PipeWire reports: subject id, key, type, and value. remove `MetadataEntry.SubjectSerial`, per-entry startup resolution, orphan tracking, and the per-node subject join. keep settings metadata decoding and quantum echo confirmation. settle the loss of per-node policy convenience with Michael before implementing, and revise the canon to allow a plainly labeled protocol subject id in a raw diagnostic table.

## why

`internal/pipewire/objects.go:477` through `metadataProperty` maintain pending keys, resolve them at startup, orphan them on removal, and distinguish new properties from old orphaned ones under a reused id. Their production consumer is the inspector (`defaultsNaming` and `defaultsRows` in `internal/ui/inspector.go:475`); this state never routes anything or recognizes a workspace record.

Displaying the reported subject id without resolving it to a current node removes the identity claim that requires the machinery. Do not replace it with a naive id-to-current-node join. A global metadata table remains useful for inspection; the deliberate cost is losing automatic per-node attribution. The existing JSON-value name search is also a display convenience, not proof of subject identity, and should not become a replacement lifetime heuristic.
