# NewAPI Video Tasks

该插件使用与 NewAPI Video Generations Channel 2 相同的创建接口 `POST /v1/video/generations`，但查询走 `GET /v1/video/tasks/{task_id}`。它适用于小力孩等实现这一查询路径的网关，不能与 `/v1/video/generations/{task_id}` 的 Channel 2 混用。

## 接口与鉴权

{{OPERATIONS}}

```http
POST {channel_base_url}/v1/video/generations
GET  {channel_base_url}/v1/video/tasks/{task_id}
Authorization: Bearer <CHANNEL_API_KEY>
Content-Type: application/json
```

## 模型与约束来源

可用模型、每种素材上限、时长范围、音频能力、分辨率和计费由该网关映射的上游决定。插件没有通用白名单。

## 参数与字段映射

{{PARAMETERS}}

实际字段严格对齐小力孩官方视频适配文档：`model`、`prompt`、`seconds`、`aspect_ratio`、`ratio`（与 `aspect_ratio` 同值），以及非空时的 `images` / `videos` / `audios`。参考素材发送为 `data:<mime>;base64,...`（图片最多 9 张，视频/音频各最多 3 个）。原文件合计按约 80MB 安全预算控制，超出先压缩图片再发送，避免超过官方 128MB 请求体上限。空的 `videos`、`audios` 或 `images` 不发送。时长默认 6 秒并发送为字符串，比例默认 `9:16`。不发送 `image`、`image_urls`、`resolution`、`generate_audio` 等额外字段。

## 完整多模态请求

```bash
curl "{channel_base_url}/v1/video/generations" \
  -H "Authorization: Bearer <CHANNEL_API_KEY>" \
  -H "Content-Type: application/json" \
  -d '{
    "model":"YOUR_VIDEO_MODEL",
    "prompt":"人物参考第一张图，镜头运动参考视频，节奏跟随音频",
    "seconds":"10",
    "aspect_ratio":"9:16",
    "ratio":"9:16",
    "images":["data:image/jpeg;base64,..."],
    "videos":["data:video/mp4;base64,..."],
    "audios":["data:audio/mpeg;base64,..."]
  }'
```

## 任务状态与结果

```json
{"id":"task_xxx","status":"queued"}
```

```bash
curl "{channel_base_url}/v1/video/tasks/task_xxx" \
  -H "Authorization: Bearer <CHANNEL_API_KEY>"
```

成功状态包括 `succeeded`、`completed` 和 `SUCCESS`。成功结果可来自 `data` 数组中的 `url`（如 `data.0.url`）或常见顶层 / 嵌套 URL 字段。当前没有取消路径。

## 官方资料与边界

- 以连接网关的接口文档为准；创建成功后必须用 `/v1/video/tasks/{id}` 查询，不能改查 `/v1/video/generations/{id}`。
- 参考图/音视频以 data URI 内嵌发送，不依赖上游拉取影策公网地址。

{{CONTRACT}}
