# Automated Bruin Web Tutorials

This directory contains a first pass at fully automated Bruin Web tutorial rendering.

The current flow is:

1. build the current frontend with `pnpm build`
2. generate narration audio segment-by-segment with Chatterbox TTS from Node.js
3. launch the real Bruin binary against a sandboxed workspace fixture
4. replay scripted browser interactions with Playwright while recording video
5. generate a title-card thumbnail and intro card
6. mux the recorded browser video and generated narration into a final mp4

## Tutorials

`tutorial-01-quick-tour.json` covers:

- opening an asset
- inspecting data
- materializing an asset
- renaming an asset
- reloading to show the rename persisted

`tutorial-02-build-a-pipeline.json` covers:

- creating a new pipeline from an empty workspace
- creating SQL assets on the canvas
- writing SQL directly in the editor
- adding a downstream join asset with explicit `depends`
- showing the graph connect those assets automatically

The narration is intentionally casual.

## Requirements

- a built Bruin binary, passed via `BRUIN_E2E_BINARY` if needed
- `ffmpeg`
- a reachable Chatterbox TTS API

By default the renderer uses `http://127.0.0.1:4123/v1/audio/speech`. You can override it with `CHATTERBOX_TTS_API_URL`.

This avoids requiring a local Python TTS integration and keeps the tutorial pipeline fully Node-based.

## Run it

From the repo root:

```bash
BRUIN_E2E_BINARY="$(pwd)/bruin" corepack pnpm --dir web tutorial:quick-tour
BRUIN_E2E_BINARY="$(pwd)/bruin" corepack pnpm --dir web tutorial:build-pipeline
```

Outputs are written under `web/.tmp/tutorials/tutorial-01-quick-tour/`.

The renderer now prefers the repo-root `./bruin` binary automatically if `BRUIN_E2E_BINARY` is not set.
