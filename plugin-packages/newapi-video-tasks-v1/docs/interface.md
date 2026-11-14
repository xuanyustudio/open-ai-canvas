# NewAPI Video Tasks（/v1/video/tasks 查询） 接口字段

## 协议身份

- 插件 ID：`newapi-video-tasks-v1`。
- Provider ID：`newapi-video-tasks`。
- 能力：`video`。
- 默认 Base URL：`http://127.0.0.1:3000`。
- 鉴权驱动：`bearer`。
- 创建：`POST /v1/video/generations`。
- 查询：`GET /v1/video/tasks/{{taskId}}`。

## 配置字段

| 字段 | 类型 | 必填 | 含义 |
| --- | --- | --- | --- |
| `apiKey` | secret | 是 | API Key |

## 统一字段映射

| 统一字段 | 类型 | 必填 | 上游映射 | 说明 |
| --- | --- | --- | --- | --- |
| `model` | string | 是 | `model` | 上游模型 ID，按 Canvas 当前选用的模型发送，不写死网关示例名。 |
| `prompt` | string | 是 | `prompt` | 视频生成提示词。 |
| `images` | media[] | 否 | `images` | 参考图，发送为 data URI（最多 9 张）；空数组不发送。超预算时先压缩。 |
| `videos` | media[] | 否 | `videos` | 参考视频，发送为 data URI（最多 3 个）；空则省略。 |
| `audios` | media[] | 否 | `audios` | 参考音频，发送为 data URI（最多 3 个）；空则省略。 |
| `duration` | integer | 否 | `seconds` | 时长秒数，发送为 seconds 字符串。 |
| `aspectRatio` | string | 否 | `aspect_ratio / ratio` | 画幅比例，同时发送 aspect_ratio 与 ratio，缺省 9:16。 |
| `providerOptions` | object | 否 | `provider-specific fields` | 插件命名空间内的厂商扩展字段。 |

## 上游请求模板逐字段清单

下表由插件请求模板生成，覆盖 body、query、headers 和 multipart 文件声明中的每个字段。

| 上游位置 | 值或转换表达式 |
| --- | --- |
| `create.method` | `"POST"` |
| `create.path` | `"/v1/video/generations"` |
| `create.contentType` | `"application/json"` |
| `create.body.model` | `{"$ref":"request.model"}` |
| `create.body.prompt` | `{"$ref":"request.prompt"}` |
| `create.body.seconds` | `{"$toString":{"$ref":"request.duration"}}` |
| `create.body.aspect_ratio` | `{"$coalesce":[{"$ref":"request.aspectRatio"},"9:16"]}` |
| `create.body.ratio` | `{"$coalesce":[{"$ref":"request.aspectRatio"},"9:16"]}` |
| `create.body.images` | `{"$omitEmpty":{"$map":{"from":{"$sortByOrder":{"$ref":"request.images"}},"as":"media","in":{"$ref":"media.value"}}}}` |
| `create.body.videos` | `{"$omitEmpty":{"$map":{"from":{"$sortByOrder":{"$ref":"request.videos"}},"as":"media","in":{"$ref":"media.value"}}}}` |
| `create.body.audios` | `{"$omitEmpty":{"$map":{"from":{"$sortByOrder":{"$ref":"request.audios"}},"as":"media","in":{"$ref":"media.value"}}}}` |
| `poll.method` | `"GET"` |
| `poll.path` | `"/v1/video/tasks/{{taskId}}"` |

## Provider 扩展键

- 无额外扩展键。

动态模型或工作流允许使用文档声明的完整 `parameters/input/extra_body` 对象；该对象是协议本身的开放 schema，不会被宿主裁剪。

## 响应映射逐字段清单

| 映射位置 | 上游路径或转换表达式 |
| --- | --- |
| `response.taskId` | `{"$coalesce":[{"$ref":"response.data.task_id"},{"$ref":"response.data.taskId"},{"$ref":"response.task_id"},{"$ref":"response.taskId"},{"$ref":"response.data.id"},{"$ref":"response.id"},{"$ref":"taskId"}]}` |
| `response.status` | `{"$coalesce":[{"$ref":"response.status"},{"$ref":"response.state"},{"$ref":"response.data.status"},"pending"]}` |
| `response.message` | `{"$coalesce":[{"$coalesce":[{"$ref":"response.error.message"},{"$ref":"response.message"},{"$ref":"response.fail_reason"},{"$ref":"response.data.fail_reason"},{"$ref":"response.data.error.message"}]},{"$ref":"response.error"}]}` |
| `response.videos` | `{"$coalesce":[{"$map":{"from":{"$ref":"response.data"},"as":"item","in":{"$coalesce":[{"$ref":"item.url"},{"$ref":"item.video_url"},{"$ref":"item.videoUrl"},{"$ref":"item.output_url"},{"$ref":"item.outputUrl"}]}}},{"$ref":"response.data.result_url"},{"$ref":"response.data.video_url"},{"$ref":"response.data.output_url"},{"$ref":"response.data.url"},{"$ref":"response.data.metadata.url"},{"$ref":"response.data.data.video_url"},{"$ref":"response.data.data.output_url"},{"$ref":"response.data.data.result_url"},{"$ref":"response.data.data.url"},{"$ref":"response.data.data.metadata.url"},{"$ref":"response.video_url"},{"$ref":"response.videoUrl"},{"$ref":"response.result_url"},{"$ref":"response.output_url"},{"$ref":"response.url"},{"$ref":"response.metadata.url"},{"$ref":"response.output.url"}]}` |
| `response.errorPaths[0]` | `"error.code"` |
| `response.resultEphemeral` | `true` |

## 响应与错误

插件把上游 task/status/text/media/usage 映射为统一结果。临时媒体 URL 标记为 ephemeral，由宿主立即下载持久化。HTTP 错误、业务 code 和 error object 保持失败语义，不包装成成功。

## 兼容边界

该协议创建路径与 NewAPI Video Generations Channel 2 相同（POST /v1/video/generations），但查询走 GET /v1/video/tasks/{taskId}，适用于小力孩等使用该查询路径的网关。创建体对齐小力孩官方视频适配文档：只发送 model、prompt、aspect_ratio、ratio、seconds，以及非空时的 images/videos/audios；缺省画幅 9:16。参考素材发送为 data URI（宿主会读取本地资源或拉取 URL 后内嵌，超 80MB 原图预算时压缩图片），避免上游无法访问 Canvas 公网地址。不能与 newapi-channel-2 混用，也不能根据模型名猜测。

<!-- YINGCE_MANIFEST_CONTRACT_START -->
## Manifest 完整接口定义

以下 JSON 与插件包内实际 `manifest.json` 逐字段一致，覆盖插件身份、权限、配置、鉴权、参数、校验、创建、Agent、查询、取消、结果下载、响应和 Agent 响应映射。`documentation` 字段的值就是当前完整文档；为避免文档在自身内部无限递归，JSON 中仅用等义占位文本表示正文。

```json
{
  "apiVersion": "yingce.plugin/v2",
  "id": "newapi-video-tasks-v1",
  "name": "NewAPI Video Tasks（/v1/video/tasks 查询）",
  "version": "2.0.0",
  "author": "NewAPI / Canvas",
  "description": "NewAPI Video Tasks（/v1/video/tasks 查询） 独立请求协议插件。",
  "documentation": "<当前插件的完整 documentation，由 README.md 与 docs/interface.md 拼接而成；为避免 JSON 递归，此处不重复展开正文。>",
  "permissions": [
    "generation.run",
    "media.read"
  ],
  "configuration": {
    "fields": [
      {
        "name": "apiKey",
        "type": "secret",
        "label": "API Key",
        "required": true
      }
    ]
  },
  "contributes": {
    "providers": [
      {
        "id": "newapi-video-tasks",
        "label": "NewAPI Video Tasks（/v1/video/tasks 查询）",
        "capabilities": [
          "video"
        ],
        "scopes": [
          "admin.system-channel",
          "user.custom-channel",
          "canvas",
          "creation",
          "agent"
        ],
        "baseUrl": "http://127.0.0.1:3000",
        "requiresPublicMediaUrls": false,
        "auth": {
          "type": "bearer",
          "field": "apiKey"
        },
        "parameters": [
          {
            "name": "model",
            "type": "string",
            "required": true,
            "mapping": "model",
            "description": "上游模型 ID，按 Canvas 当前选用的模型发送，不写死网关示例名。"
          },
          {
            "name": "prompt",
            "type": "string",
            "required": true,
            "mapping": "prompt",
            "description": "视频生成提示词。"
          },
          {
            "name": "images",
            "type": "media[]",
            "required": false,
            "mapping": "images",
            "description": "参考图，发送为 data URI（最多 9 张）；空数组不发送。超预算时先压缩。"
          },
          {
            "name": "videos",
            "type": "media[]",
            "required": false,
            "mapping": "videos",
            "description": "参考视频，发送为 data URI（最多 3 个）；空则省略。"
          },
          {
            "name": "audios",
            "type": "media[]",
            "required": false,
            "mapping": "audios",
            "description": "参考音频，发送为 data URI（最多 3 个）；空则省略。"
          },
          {
            "name": "duration",
            "type": "integer",
            "required": false,
            "mapping": "seconds",
            "description": "时长秒数，发送为 seconds 字符串。"
          },
          {
            "name": "aspectRatio",
            "type": "string",
            "required": false,
            "mapping": "aspect_ratio / ratio",
            "description": "画幅比例，同时发送 aspect_ratio 与 ratio，缺省 9:16。"
          },
          {
            "name": "providerOptions",
            "type": "object",
            "required": false,
            "mapping": "provider-specific fields",
            "description": "插件命名空间内的厂商扩展字段。"
          }
        ],
        "create": {
          "method": "POST",
          "path": "/v1/video/generations",
          "contentType": "application/json",
          "body": {
            "model": {
              "$ref": "request.model"
            },
            "prompt": {
              "$ref": "request.prompt"
            },
            "seconds": {
              "$toString": {
                "$ref": "request.duration"
              }
            },
            "aspect_ratio": {
              "$coalesce": [
                {
                  "$ref": "request.aspectRatio"
                },
                "9:16"
              ]
            },
            "ratio": {
              "$coalesce": [
                {
                  "$ref": "request.aspectRatio"
                },
                "9:16"
              ]
            },
            "images": {
              "$omitEmpty": {
                "$map": {
                  "from": {
                    "$sortByOrder": {
                      "$ref": "request.images"
                    }
                  },
                  "as": "media",
                  "in": {
                    "$ref": "media.value"
                  }
                }
              }
            },
            "videos": {
              "$omitEmpty": {
                "$map": {
                  "from": {
                    "$sortByOrder": {
                      "$ref": "request.videos"
                    }
                  },
                  "as": "media",
                  "in": {
                    "$ref": "media.value"
                  }
                }
              }
            },
            "audios": {
              "$omitEmpty": {
                "$map": {
                  "from": {
                    "$sortByOrder": {
                      "$ref": "request.audios"
                    }
                  },
                  "as": "media",
                  "in": {
                    "$ref": "media.value"
                  }
                }
              }
            }
          }
        },
        "poll": {
          "method": "GET",
          "path": "/v1/video/tasks/{{taskId}}"
        },
        "response": {
          "taskId": {
            "$coalesce": [
              {
                "$ref": "response.data.task_id"
              },
              {
                "$ref": "response.data.taskId"
              },
              {
                "$ref": "response.task_id"
              },
              {
                "$ref": "response.taskId"
              },
              {
                "$ref": "response.data.id"
              },
              {
                "$ref": "response.id"
              },
              {
                "$ref": "taskId"
              }
            ]
          },
          "status": {
            "$coalesce": [
              {
                "$ref": "response.status"
              },
              {
                "$ref": "response.state"
              },
              {
                "$ref": "response.data.status"
              },
              "pending"
            ]
          },
          "message": {
            "$coalesce": [
              {
                "$coalesce": [
                  {
                    "$ref": "response.error.message"
                  },
                  {
                    "$ref": "response.message"
                  },
                  {
                    "$ref": "response.fail_reason"
                  },
                  {
                    "$ref": "response.data.fail_reason"
                  },
                  {
                    "$ref": "response.data.error.message"
                  }
                ]
              },
              {
                "$ref": "response.error"
              }
            ]
          },
          "videos": {
            "$coalesce": [
              {
                "$map": {
                  "from": {
                    "$ref": "response.data"
                  },
                  "as": "item",
                  "in": {
                    "$coalesce": [
                      {
                        "$ref": "item.url"
                      },
                      {
                        "$ref": "item.video_url"
                      },
                      {
                        "$ref": "item.videoUrl"
                      },
                      {
                        "$ref": "item.output_url"
                      },
                      {
                        "$ref": "item.outputUrl"
                      }
                    ]
                  }
                }
              },
              {
                "$ref": "response.data.result_url"
              },
              {
                "$ref": "response.data.video_url"
              },
              {
                "$ref": "response.data.output_url"
              },
              {
                "$ref": "response.data.url"
              },
              {
                "$ref": "response.data.metadata.url"
              },
              {
                "$ref": "response.data.data.video_url"
              },
              {
                "$ref": "response.data.data.output_url"
              },
              {
                "$ref": "response.data.data.result_url"
              },
              {
                "$ref": "response.data.data.url"
              },
              {
                "$ref": "response.data.data.metadata.url"
              },
              {
                "$ref": "response.video_url"
              },
              {
                "$ref": "response.videoUrl"
              },
              {
                "$ref": "response.result_url"
              },
              {
                "$ref": "response.output_url"
              },
              {
                "$ref": "response.url"
              },
              {
                "$ref": "response.metadata.url"
              },
              {
                "$ref": "response.output.url"
              }
            ]
          },
          "errorPaths": [
            "error.code"
          ],
          "resultEphemeral": true
        }
      }
    ]
  }
}
```
<!-- YINGCE_MANIFEST_CONTRACT_END -->
