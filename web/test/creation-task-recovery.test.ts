import { describe, expect, test } from "bun:test";

import { creationMessageNeedsResultSync, reattachMissingCreationTaskMessages, reconcileCreationTaskMessages, type PersistedCreationTask } from "@/pages/create/creation-conversations";
import type { CreationConversation, CreationMessage } from "@/pages/create/creation-types";
import { pendingCreationTaskIds } from "@/services/creation-conversation-store";
import { recoverCreationTextTask } from "@/services/creation-text-task-recovery";

const runtime = { recoverCreationTextTask } as unknown as Parameters<typeof reconcileCreationTaskMessages>[0];
const at = "2026-10-27T11:00:00.000Z";

function task(overrides: Partial<PersistedCreationTask> = {}): PersistedCreationTask {
    return { id: "task-1", type: "canvas_video", status: "running", prompt: "镜头", attempts: 1, createdAt: at, updatedAt: "2026-10-27T11:02:00.000Z", ...overrides };
}

function assistant(overrides: Partial<CreationMessage> = {}): CreationMessage {
    return { id: "assistant-1", role: "assistant", mode: "video", content: "", createdAt: at, taskIds: ["task-1"], ...overrides };
}

function conversations(message: CreationMessage): CreationConversation[] {
    return [{ id: "conversation-1", title: "镜头", updatedAt: at, messages: [{ id: "user-1", role: "user", mode: "video", content: "镜头", createdAt: at }, message] }];
}

const messageOf = (value: CreationConversation[]) => value[0].messages[1];

describe("首页创作：页面没跟上 ≠ 生成失败", () => {
    test("轮询中断后任务仍在跑：失败消息回到生成中，并带上真实阶段与进度", () => {
        const next = messageOf(reconcileCreationTaskMessages(runtime, conversations(assistant({ status: "error", errorKind: "sync", error: "网络异常。", content: "生成失败" })), [task({ stage: "上游生成中", progress: 40 })]));
        expect(next).toMatchObject({ status: "pending", generationStatus: "running", generationStage: "上游生成中", generationProgress: 40 });
        expect(next.error).toBeUndefined();
        expect(next.errorKind).toBeUndefined();
        expect(next.content).toBe("");
    });

    test("旧版本未分类的“网络异常”失败，在任务成功后补回作品", () => {
        const legacy = assistant({ status: "error", error: "网络异常。", content: "生成失败" });
        expect(creationMessageNeedsResultSync(legacy)).toBe(true);
        const next = messageOf(reconcileCreationTaskMessages(runtime, conversations(legacy), [task({ status: "succeeded", creationResultStorageKeys: ["resource:video-1"] })]));
        expect(next).toMatchObject({ status: "done", content: "视频已生成", resultStorageKeys: ["resource:video-1"] });
        expect(next.error).toBeUndefined();
    });

    test("任务成功但结果暂时读不出来：保持“结果待同步”，不引导重新生成", () => {
        const next = messageOf(reconcileCreationTaskMessages(runtime, conversations(assistant({ status: "pending", syncInterrupted: true })), [task({ status: "succeeded", creationError: "Failed to fetch" })]));
        expect(next).toMatchObject({ status: "error", errorKind: "sync", error: "网络异常。" });
        expect(next.syncInterrupted).toBeUndefined();
        expect(creationMessageNeedsResultSync(next)).toBe(true);
    });

    test("后端确认失败：标记为任务失败；作品已生成可重新保存时保留入口", () => {
        const next = messageOf(reconcileCreationTaskMessages(runtime, conversations(assistant({ status: "pending" })), [task({ status: "failed", error: "作品已生成，但保存未完成：上传 OSS", mediaStage: "upload", canRecoverMedia: true })]));
        expect(next).toMatchObject({ status: "error", errorKind: "task", mediaRecoverable: true });
        expect(creationMessageNeedsResultSync(next)).toBe(false);
    });

    test("后端已确认失败的消息不会被之后的快照改写", () => {
        const source = conversations(assistant({ status: "error", errorKind: "task", error: "模型服务处理失败" }));
        expect(reconcileCreationTaskMessages(runtime, source, [task({ status: "succeeded", creationResultStorageKeys: ["resource:video-1"] })])).toBe(source);
    });

    test("阶段和进度没有变化时不产生新对象，避免每次轮询都重写本地记录", () => {
        const source = conversations(assistant({ status: "pending", generationStatus: "running", generationStage: "上游生成中", generationProgress: 40 }));
        expect(reconcileCreationTaskMessages(runtime, source, [task({ stage: "上游生成中", progress: 40 })])).toBe(source);
    });

    test("多图任务要等全部任务快照到齐才下结论", () => {
        const source = conversations(assistant({ mode: "image", status: "pending", taskIds: ["task-a", "task-b"] }));
        const first = task({ id: "task-a", type: "canvas_image", status: "succeeded", creationResultStorageKeys: ["resource:a"], clientContext: { conversationId: "conversation-1", messageId: "assistant-1", batchIndex: 0, batchCount: 2 } });
        expect(reconcileCreationTaskMessages(runtime, source, [first])).toBe(source);
        const second = task({ id: "task-b", type: "canvas_image", status: "failed", error: "上游拒绝", clientContext: { conversationId: "conversation-1", messageId: "assistant-1", batchIndex: 1, batchCount: 2 } });
        expect(messageOf(reconcileCreationTaskMessages(runtime, source, [first, second]))).toMatchObject({ status: "done", resultStorageKeys: ["resource:a"], content: "1 张图片已生成，1 张失败" });
    });

    test("手动重新获取可以重算已完成但没有可预览结果的消息", () => {
        const source = conversations(assistant({ status: "done", content: "视频已生成" }));
        const snapshot = [task({ status: "succeeded", creationResultStorageKeys: ["resource:video-1"] })];
        expect(reconcileCreationTaskMessages(runtime, source, snapshot)).toBe(source);
        expect(messageOf(reconcileCreationTaskMessages(runtime, source, snapshot, { reviveMessageIds: new Set(["assistant-1"]) }))).toMatchObject({ status: "done", resultStorageKeys: ["resource:video-1"] });
    });

    test("文本消息恢复监听拿到进行中状态后收起连接中断提示", () => {
        const source = conversations(assistant({ mode: "text", status: "pending", syncInterrupted: true, content: "第一段" }));
        const next = messageOf(reconcileCreationTaskMessages(runtime, source, [task({ type: "canvas_text" })]));
        expect(next.syncInterrupted).toBeUndefined();
        expect(next).toMatchObject({ status: "pending", content: "第一段" });
    });
});

describe("首页创作恢复监听范围", () => {
    test("生成中、结果待同步与旧版本失败继续跟进；后端确认失败的不再订阅", () => {
        const stored = [
            {
                id: "conversation-1",
                messages: [
                    { id: "pending", role: "assistant" as const, mode: "video", status: "pending", taskIds: ["task-pending"] },
                    { id: "sync", role: "assistant" as const, mode: "video", status: "error", errorKind: "sync", taskIds: ["task-sync"] },
                    { id: "legacy", role: "assistant" as const, mode: "video", status: "error", taskIds: ["task-legacy"] },
                    { id: "failed", role: "assistant" as const, mode: "video", status: "error", errorKind: "task", taskIds: ["task-failed"] },
                    { id: "done", role: "assistant" as const, mode: "video", status: "done", taskIds: ["task-done"] },
                ],
            },
        ];
        expect(pendingCreationTaskIds(stored)).toEqual(["task-pending", "task-sync", "task-legacy"]);
    });
});

describe("首页创作：从服务端任务补回丢失的镜头", () => {
    const conversationId = "conversation-1";
    const longPrompt = "镜头提示词".repeat(80);

    test("丢失的成功任务会按时间顺序补回镜头，并带上可播放的存储键", () => {
        const existing = conversations(assistant({ status: "pending", generationStatus: "running", generationProgress: 46 }));
        const missing = task({
            id: "task-minimax",
            status: "succeeded",
            prompt: longPrompt.slice(0, 80),
            createdAt: "2026-11-17T00:58:53.000Z",
            updatedAt: "2026-11-17T01:05:53.000Z",
            clientContext: { conversationId, messageId: "assistant-minimax" },
            creationResultStorageKeys: ["resource:video-minimax"],
        });
        const attached = reattachMissingCreationTaskMessages(existing, [
            task({ clientContext: { conversationId, messageId: "assistant-1" } }),
            missing,
        ]);
        expect(attached[0].messages.map((message) => message.id)).toEqual(["user-1", "assistant-1", "assistant-minimax:user", "assistant-minimax"]);
        const next = reconcileCreationTaskMessages(runtime, attached, [missing]);
        const shot = next[0].messages[3];
        expect(shot).toMatchObject({ id: "assistant-minimax", status: "done", resultStorageKeys: ["resource:video-minimax"] });
        expect(next[0].messages[2].content).toBe(longPrompt.slice(0, 80));
    });

    test("丢失的生成中任务会补回同一任务 ID，并进入恢复监听", () => {
        const source: CreationConversation[] = [{ id: conversationId, title: "空对话", updatedAt: at, messages: [] }];
        const running = task({
            id: "task-seedance",
            status: "running",
            stage: "上游生成中",
            progress: 38,
            clientContext: { conversationId, messageId: "assistant-seedance" },
        });
        const attached = reattachMissingCreationTaskMessages(source, [running]);
        expect(attached[0].messages.map((message) => message.id)).toEqual(["assistant-seedance:user", "assistant-seedance"]);
        const next = reconcileCreationTaskMessages(runtime, attached, [running]);
        expect(next[0].messages[1]).toMatchObject({ status: "pending", taskIds: ["task-seedance"], generationStatus: "running", generationProgress: 38 });
        expect(pendingCreationTaskIds(next)).toEqual(["task-seedance"]);
    });

    test("本地已有的消息不会被重复插入", () => {
        const source = conversations(assistant({ status: "pending" }));
        const same = task({ clientContext: { conversationId, messageId: "assistant-1" } });
        expect(reattachMissingCreationTaskMessages(source, [same])).toBe(source);
    });

    test("其他对话的任务不会写进当前对话", () => {
        const source = conversations(assistant({ status: "pending" }));
        const other = task({
            id: "task-other",
            status: "succeeded",
            clientContext: { conversationId: "conversation-other", messageId: "assistant-other" },
            creationResultStorageKeys: ["resource:other"],
        });
        const attached = reattachMissingCreationTaskMessages(source, [other]);
        expect(attached.map((conversation) => conversation.id)).toEqual(["conversation-other", conversationId]);
        expect(attached.find((conversation) => conversation.id === conversationId)?.messages.map((message) => message.id)).toEqual(["user-1", "assistant-1"]);
    });

    test("整段对话丢失时，按同一 conversationId 重建历史项", () => {
        const running = task({
            id: "task-seedance",
            status: "running",
            createdAt: "2026-11-17T00:56:53.000Z",
            clientContext: { conversationId: "lost-conversation", messageId: "assistant-seedance" },
        });
        const done = task({
            id: "task-minimax",
            status: "succeeded",
            createdAt: "2026-11-17T00:58:53.000Z",
            clientContext: { conversationId: "lost-conversation", messageId: "assistant-minimax" },
            creationResultStorageKeys: ["resource:video-minimax"],
        });
        const attached = reattachMissingCreationTaskMessages([], [done, running]);
        expect(attached).toHaveLength(1);
        expect(attached[0].id).toBe("lost-conversation");
        expect(attached[0].messages.map((message) => message.id)).toEqual([
            "assistant-seedance:user",
            "assistant-seedance",
            "assistant-minimax:user",
            "assistant-minimax",
        ]);
    });

    test("被后续重试取代的失败尝试不会再单独补成镜头", () => {
        const latest = task({
            id: "task-latest",
            status: "running",
            retryOf: "task-retry",
            createdAt: "2026-11-17T00:56:53.000Z",
            clientContext: { conversationId, messageId: "assistant-latest" },
        });
        const mid = task({
            id: "task-retry",
            status: "failed",
            retryOf: "task-first",
            createdAt: "2026-11-17T00:53:30.000Z",
            clientContext: { conversationId, messageId: "assistant-retry" },
        });
        const first = task({
            id: "task-first",
            status: "failed",
            createdAt: "2026-11-17T00:53:09.000Z",
            clientContext: { conversationId, messageId: "assistant-first" },
        });
        const attached = reattachMissingCreationTaskMessages([], [first, mid, latest]);
        expect(attached[0].messages.filter((message) => message.role === "assistant").map((message) => message.id)).toEqual(["assistant-latest"]);
        expect(attached[0].messages[1].taskIds).toEqual(["task-latest"]);
    });

    test("列表提示词被截断时，沿用同一对话里已有的完整用户提示词", () => {
        const source = conversations(assistant({ status: "pending" }));
        source[0].messages[0].content = longPrompt;
        const missing = task({
            id: "task-minimax",
            status: "succeeded",
            prompt: longPrompt.slice(0, 80),
            createdAt: "2026-11-17T00:58:53.000Z",
            clientContext: { conversationId, messageId: "assistant-minimax" },
            creationResultStorageKeys: ["resource:video-minimax"],
        });
        const attached = reattachMissingCreationTaskMessages(source, [missing]);
        expect(attached[0].messages[2].content).toBe(longPrompt);
    });
});
