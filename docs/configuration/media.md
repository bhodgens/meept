# Media generation

Bundled `config/models.json5` is the base catalog. `~/.meept/models.json5` overlays it. Your chat slots stay. Missing `image_model` / image providers still come from the bundle.

## Slots

```json5
{
  image_model: "xai/grok-imagine-image-2.0",
  video_model: "xai/grok-imagine-video",
}
```

Or point at a local ComfyUI model:

```json5
{
  image_model: "comfyui/flux-dev",
}
```

## Define a model

Same block as an LLM. Add capability `image` or `video`.

```json5
"comfyui": {
  "api": "comfyui",
  "options": { "baseURL": "http://127.0.0.1:8188" },
  "models": {
    "flux-dev": {
      "name": "flux-dev",
      "capabilities": ["image"],
      "workflow": "~/.meept/workflows/flux_dev.json"
    }
  }
}
```

A hosted Grok model:

```json5
"xai": {
  "api": "openai",
  "options": { "baseURL": "https://api.x.ai/v1", "apiKey": "${XAI_API_KEY}" },
  "models": {
    "grok-imagine-image-2.0": {
      "name": "grok-imagine-image-2.0",
      "capabilities": ["image"]
    }
  }
}
```

A custom HTTP API:

```json5
"my-api": {
  "api": "http",
  "options": { "baseURL": "https://api.example.com", "apiKey": "${EXAMPLE_API_KEY}" },
  "models": {
    "v1": {
      "name": "v1",
      "capabilities": ["image"],
      "generation_url": "https://api.example.com/v1/images",
      "body_template": { "prompt": "{{prompt}}", "model": "{{model}}" },
      "response_url_json_path": "data.0.url"
    }
  }
}
```

## Transports

Inferred from `api` + capability, or set `api` on the model:

| api | Call |
|-----|------|
| `openai` + `image` | `POST {baseURL}/images/generations` |
| `openai` + `video` | `POST {baseURL}/videos/generations` |
| `gemini` | Google `generateContent` |
| `comfyui` | `/prompt` + `/history` (needs `workflow`) |
| `infsh` | `infsh app run` (`image_app` / `video_app` or model name) |
| `http` | `generation_url` + `body_template` |

## Tools

`generate_image` / `generate_video` take `model` as `provider/id` or an alias (`image`, `video`). Empty uses `image_model` / `video_model`.

Files write under `media.output_dir` in `meept.json5` (`~/.meept/media`). Not the daemon CWD.

## Alias failover

```json5
"image": {
  "models": ["comfyui/flux-dev", "xai/grok-imagine-image-2.0"],
  "timeout": 60,
  "max_fails": 2
}
```
