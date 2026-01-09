# Excalidraw Query Battery — Grounded Expected Symbols

All symbols verified against excalidraw/excalidraw source on GitHub (2026-04-05).

## Investigation 1: Element rendering (8 symbols)

Query keyword: `"render canvas draw element scene static"`
Query intent: `"how are elements rendered on the canvas"`

Expected symbols:
- `drawElementOnCanvas` — packages/element/src/renderElement.ts — draws single element on 2D canvas context
- `renderElement` — packages/element/src/renderElement.ts — top-level element render dispatcher
- `generateElementCanvas` — packages/element/src/renderElement.ts — creates offscreen canvas for element
- `generateElementWithCanvas` — packages/element/src/renderElement.ts — pairs element with its cached canvas
- `drawElementFromCanvas` — packages/element/src/renderElement.ts — blits cached canvas to scene
- `_renderStaticScene` — packages/excalidraw/renderer/staticScene.ts — renders full static scene
- `renderStaticScene` — packages/excalidraw/renderer/staticScene.ts — public entry point for static render
- `renderElementToSvg` — packages/excalidraw/renderer/staticSvgScene.ts — SVG export renderer

## Investigation 2: Export and save (8 symbols)

Query keyword: `"export save canvas PNG SVG blob serialize"`
Query intent: `"how are drawings exported and saved"`

Expected symbols:
- `exportCanvas` — packages/excalidraw/data/index.ts — main export dispatcher (PNG/SVG/clipboard)
- `exportToCanvas` — packages/utils/src/export.ts — public API: elements → canvas
- `exportToBlob` — packages/utils/src/export.ts — public API: elements → blob
- `exportToSvg` — packages/utils/src/export.ts — public API: elements → SVG element
- `prepareElementsForExport` — packages/excalidraw/data/index.ts — filters/clones elements for export
- `encodePngMetadata` — packages/excalidraw/data/image.ts — embeds scene JSON in PNG metadata
- `serializeAsClipboardJSON` — packages/excalidraw/clipboard.ts — serializes for clipboard
- `exportToBackend` — excalidraw-app/data/index.ts — uploads drawing to backend

## Investigation 3: Undo/redo history (8 symbols)

Query keyword: `"undo redo history state change delta stack"`
Query intent: `"how does undo and redo work"`

Expected symbols:
- `History` — packages/excalidraw/history.ts — main history class with undo/redo stacks
- `HistoryDelta` — packages/excalidraw/history.ts — delta subclass for history entries
- `undo` — packages/excalidraw/history.ts — pops undo stack, applies delta
- `redo` — packages/excalidraw/history.ts — pops redo stack, applies delta
- `record` — packages/excalidraw/history.ts — records new delta to undo stack
- `perform` — packages/excalidraw/history.ts — core undo/redo loop implementation
- `applyTo` — packages/excalidraw/history.ts — applies delta to elements and appState
- `redoStack` — packages/excalidraw/history.ts — the redo stack property

## Investigation 4: Real-time collaboration (8 symbols)

Query keyword: `"collaboration socket portal room broadcast sync"`
Query intent: `"how does real-time collaboration work"`

Expected symbols:
- `Portal` — excalidraw-app/collab/Portal.tsx — WebSocket portal for collaboration
- `open` — excalidraw-app/collab/Portal.tsx — opens socket connection to room
- `_broadcastSocketData` — excalidraw-app/collab/Portal.tsx — encrypts and sends data via socket
- `broadcastScene` — excalidraw-app/collab/Portal.tsx — broadcasts element updates to room
- `CollabAPI` — excalidraw-app/collab/Collab.tsx — collaboration API interface
- `CollabState` — excalidraw-app/collab/Collab.tsx — collaboration state interface
- `collaborators` — excalidraw-app/collab/Collab.tsx — collaborators map property
- `generateCollaborationLinkData` — excalidraw-app/data/index.ts — generates shareable room link

## Investigation 5: Element creation and mutation (8 symbols)

Query keyword: `"newElement create mutate element shape flowchart"`
Query intent: `"how are new drawing elements created"`

Expected symbols:
- `newElementWith` — packages/element/src/mutateElement.ts — creates new element with overrides
- `generateElementShape` — packages/element/src/shape.ts — generates rough.js shape for element
- `create` — packages/element/src/store.ts — Store.create snapshot method
- `createNodes` — packages/element/src/flowchart.ts — creates flowchart nodes
- `addNewNode` — packages/element/src/flowchart.ts — adds new node to flowchart
- `ElementsDelta` — packages/element/src/delta.ts — tracks element changes
- `drawElementOnCanvas` — packages/element/src/renderElement.ts — draws element on canvas
- `generateLinearElementShape` — packages/element/src/bounds.ts — generates shape for linear elements
