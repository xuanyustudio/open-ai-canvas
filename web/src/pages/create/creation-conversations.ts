import { translate } from "@/i18n";
import { createClientId } from "@/lib/client-id";
import { generationErrorMessage } from "@/lib/generation-error";
import type { BackendGenerationResult } from "@/services/api/generation-task";
import type { GenerationTask } from "@/services/api/task-center";
import { creationAttachmentKind, type CreationAttachment } from "./creation-assets";
import type { CreationConversation, CreationMessage, CreationShotRailEntry } from "./creation-types";

type CreationRuntime = typeof import("./creation-runtime");
export type PersistedCreationTask = GenerationTask & { creationResultUrls?: string[]; creationResultStorageKeys?: string[]; creationError?: string };

export function newConversation(): CreationConversation {
    return { id: createClientId(), title: translate("create.conversation.untitled"), updatedAt: new Date().toISOString(), messages: [] };
}

export function newMessage(role: CreationMessage["role"], content: string, extra: Partial<CreationMessage> = {}): CreationMessage {
    return { id: createClientId(), role, content, createdAt: new Date().toISOString(), ...extra };
}

export function creationShotRail(messages: CreationMessage[]): CreationShotRailEntry[] {
    const rail: CreationShotRailEntry[] = [];
    let ordinal = 0;
    let lastIndex = -1;
    for (const message of messages) {
        const isVideo = (message.mode || "text") === "video";
        if (message.role === "user") {
            if (!isVideo) continue;
            ordinal += 1;
            rail.push({ key: message.id, ordinal, user: message });
            lastIndex = rail.length - 1;
        } else if (isVideo && lastIndex >= 0 && !rail[lastIndex].result) {
            rail[lastIndex].result = message;
        }
    }
    return rail;
}

export function creationVideoShotOrdinal(shots: CreationShotRailEntry[], item: CreationMessage): number {
    const own = shots.find((entry) => item.role === "user" ? entry.user.id === item.id : entry.result?.id === item.id);
    return own?.ordinal || 0;
}

export function completedCreationGenerationTask(runtime: CreationRuntime, input: { taskId: string; task?: GenerationTask; mode: "image" | "video"; prompt: string; result: BackendGenerationResult; conversationId: string; messageId: string; batchIndex?: number; batchCount?: number }): GenerationTask {
    const now = new Date().toISOString();
    const task = input.task ?? { id: input.taskId, type: input.mode, status: "succeeded" as const, prompt: input.prompt, attempts: 1, createdAt: now, updatedAt: now };
    return runtime.projectGenerationTaskResult({ ...task, status: "succeeded", prompt: input.prompt, clientContext: { conversationId: input.conversationId, messageId: input.messageId, ...(typeof input.batchIndex === "number" ? { batchIndex: input.batchIndex } : {}), ...(typeof input.batchCount === "number" ? { batchCount: input.batchCount } : {}) } }, input.result);
}

export function isVideoAttachment(attachment: CreationAttachment): attachment is CreationAttachment & { url: string } {
    return creationAttachmentKind(attachment) === "video";
}

export function isImageAttachment(attachment: CreationAttachment): attachment is CreationAttachment & { dataUrl: string; width?: number; height?: number } {
    return creationAttachmentKind(attachment) === "image";
}


export function attachCreationTaskContexts(tasks: GenerationTask[], conversations: CreationConversation[]) {
    const contexts = new Map<string, { prompt: string; clientContext: NonNullable<GenerationTask["clientContext"]> }>();
    for (const conversation of conversations) {
        for (const [messageIndex, message] of conversation.messages.entries()) {
            if (message.role !== "assistant" || !message.taskIds?.length) continue;
            const prompt = conversation.messages[messageIndex - 1]?.role === "user" ? conversation.messages[messageIndex - 1].content : "";
            for (const [batchIndex, taskId] of message.taskIds.entries()) contexts.set(taskId, { prompt, clientContext: { conversationId: conversation.id, messageId: message.id, batchIndex, batchCount: message.taskIds.length } });
        }
    }
    return tasks.map((task) => {
        const context = contexts.get(task.id);
        return context ? { ...task, prompt: context.prompt, clientContext: context.clientContext } : task;
    });
}

export async function materializeCreationTaskResults(runtime: CreationRuntime, tasks: GenerationTask[], signal?: AbortSignal): Promise<PersistedCreationTask[]> {
    return Promise.all(tasks.map(async (task): Promise<PersistedCreationTask> => {
        // 文本正文保存在 resultJson，不进入媒体资源化链路。
        if (task.status !== "succeeded" || !task.clientContext || task.type === "canvas_text") return task;
        try {
            const materialized = await runtime.runGenerationConsumer(signal, (managedSignal: AbortSignal) => runtime.materializeGenerationTaskAssets(task, managedSignal));
            const creationResultUrls = runtime.generationTaskMaterializedUrls(materialized);
            const creationResultStorageKeys = runtime.generationTaskMaterializedStorageKeys(materialized);
            return creationResultUrls.length || creationResultStorageKeys.length ? { ...materialized, ...(creationResultUrls.length ? { creationResultUrls } : {}), ...(creationResultStorageKeys.length ? { creationResultStorageKeys } : {}) } : materialized;
        } catch (error) {
            return { ...task, creationError: error instanceof Error ? error.message : translate("create.conversation.materializeFailed") };
        }
    }));
}

/** 失败但后端没有确认任务失败（含旧版本未分类的失败）：作品可能已生成，应提供“重新获取结果”。 */
export function creationMessageNeedsResultSync(message: Pick<CreationMessage, "status" | "errorKind" | "taskIds">) {
    return message.status === "error" && message.errorKind !== "task" && Boolean(message.taskIds?.length);
}

// 仍需用任务终态来收敛的媒体消息：生成中，或失败但后端没有确认失败。
function creationMediaMessageAwaitsTask(message: CreationMessage, revive: boolean) {
    if (message.role !== "assistant") return false;
    return revive || message.status === "pending" || creationMessageNeedsResultSync(message);
}

const clearedCreationSyncState = { syncInterrupted: undefined, errorKind: undefined, mediaRecoverable: undefined } satisfies Partial<CreationMessage>;

/** 任务仍在进行：消息回到生成中，并刷新阶段与真实进度；没有变化时返回原对象，避免无谓重渲染与持久化。 */
function creationMessageWithObservedTask(message: CreationMessage, task: GenerationTask): CreationMessage {
    const progress = typeof task.progress === "number" && Number.isFinite(task.progress) ? Math.round(task.progress) : undefined;
    const unchanged =
        message.status === "pending" &&
        !message.syncInterrupted &&
        !message.error &&
        !message.errorKind &&
        message.generationStatus === task.status &&
        message.generationStage === task.stage &&
        message.generationMediaStage === task.mediaStage &&
        message.generationProgress === progress;
    if (unchanged) return message;
    return {
        ...message,
        ...clearedCreationSyncState,
        status: "pending",
        content: message.status === "pending" ? message.content : "",
        error: undefined,
        generationStatus: task.status,
        generationStage: task.stage,
        generationMediaStage: task.mediaStage,
        generationProgress: progress,
    };
}

export function reconcileCreationTaskMessages(runtime: CreationRuntime, conversations: CreationConversation[], tasks: PersistedCreationTask[], options: { reviveMessageIds?: ReadonlySet<string> } = {}) {
    let changed = false;
    const next = conversations.map((conversation) => {
        let conversationChanged = false;
        let completedAt = conversation.updatedAt;
        const messages = conversation.messages.map((message) => {
            const taskIds = new Set(message.taskIds || []);
            const matches = tasks
                .filter((task) => taskIds.has(task.id) || (task.clientContext?.conversationId === conversation.id && task.clientContext.messageId === message.id))
                .sort((left, right) => (left.clientContext?.batchIndex || 0) - (right.clientContext?.batchIndex || 0));
            const active = matches.filter((task) => task.status === "queued" || task.status === "running");
            if (message.role === "assistant" && message.mode === "text") {
                const recovery = runtime.recoverCreationTextTask(message, matches);
                if (!recovery) {
                    // 恢复监听已重新拿到任务状态，连接中断提示可以收起；正文仍等任务终态一次性回填。
                    if (!message.syncInterrupted || !active.length) return message;
                    conversationChanged = true;
                    changed = true;
                    return { ...message, syncInterrupted: undefined };
                }
                completedAt = matches.reduce((latest, task) => conversationTimestamp(task.updatedAt) > conversationTimestamp(latest) ? task.updatedAt : latest, completedAt);
                conversationChanged = true;
                changed = true;
                return { ...message, ...recovery, syncInterrupted: undefined };
            }
            if (!creationMediaMessageAwaitsTask(message, Boolean(options.reviveMessageIds?.has(message.id)))) return message;
            const expectedTaskCount = Math.max(0, ...matches.map((task) => task.clientContext?.batchCount || 0));
            if (!matches.length || (expectedTaskCount > 0 && matches.length < expectedTaskCount)) return message;
            if (active.length) {
                const observed = creationMessageWithObservedTask(message, active[0]);
                if (observed === message) return message;
                conversationChanged = true;
                changed = true;
                return observed;
            }

            const succeeded = matches.filter((task) => task.status === "succeeded");
            const resultUrls = Array.from(new Set(succeeded.flatMap(creationTaskResultUrls)));
            const resultStorageKeys = Array.from(new Set(succeeded.flatMap(creationTaskResultStorageKeys)));
            const failedCount = matches.filter((task) => task.status !== "succeeded" || Boolean(task.creationError)).length;
            const nextTaskIds = Array.from(new Set([...(message.taskIds || []), ...matches.map((task) => task.id)]));
            completedAt = matches.reduce((latest, task) => conversationTimestamp(task.updatedAt) > conversationTimestamp(latest) ? task.updatedAt : latest, completedAt);
            conversationChanged = true;
            changed = true;

            if (resultUrls.length || resultStorageKeys.length) {
                const content = message.mode === "video" ? translate("create.status.videoGenerated") : failedCount ? translate("create.status.imagesPartiallyGenerated", { done: resultStorageKeys.length || resultUrls.length, failed: failedCount }) : translate("create.status.imageGenerated");
                return { ...message, ...clearedCreationSyncState, status: "done" as const, content, ...(resultUrls.length ? { resultUrls } : {}), ...(resultStorageKeys.length ? { resultStorageKeys } : {}), error: undefined, generationStatus: "succeeded" as const, taskIds: nextTaskIds };
            }
            if (matches.every((task) => task.status === "cancelled")) {
                return { ...message, ...clearedCreationSyncState, status: "cancelled" as const, content: translate("create.status.stopped"), error: undefined, generationStatus: "cancelled" as const, taskIds: nextTaskIds };
            }
            // 任务成功但结果没能载入（资源化失败）不是生成失败：保留“重新获取结果”，不引导重新付费生成。
            const unavailable = succeeded.find((task) => task.creationError) || succeeded[0];
            const failed = matches.find((task) => task.status === "failed");
            if (unavailable) {
                return { ...message, ...clearedCreationSyncState, status: "error" as const, content: translate("create.status.failed"), errorKind: "sync" as const, error: generationErrorMessage(unavailable.creationError || translate("create.conversation.resultUnreadable")), generationStatus: "succeeded" as const, taskIds: nextTaskIds };
            }
            return {
                ...message,
                ...clearedCreationSyncState,
                status: "error" as const,
                content: translate("create.status.failed"),
                errorKind: "task" as const,
                mediaRecoverable: matches.some((task) => task.status === "failed" && task.canRecoverMedia) || undefined,
                error: generationErrorMessage(failed?.error || translate("create.conversation.noResult")),
                generationStatus: failed ? ("failed" as const) : message.generationStatus,
                generationMediaStage: failed?.mediaStage,
                taskIds: nextTaskIds,
            };
        });
        return conversationChanged ? { ...conversation, messages, updatedAt: completedAt } : conversation;
    });
    return changed ? next : conversations;
}

function creationTaskResultUrls(task: PersistedCreationTask) {
    if (task.creationResultUrls?.length) return task.creationResultUrls;
    return [];
}

function creationTaskResultStorageKeys(task: PersistedCreationTask) {
    if (task.creationResultStorageKeys?.length) return task.creationResultStorageKeys;
    return [];
}

export function conversationTimestamp(value: string) {
    const timestamp = new Date(value).getTime();
    return Number.isFinite(timestamp) ? timestamp : 0;
}

function creationModeFromTaskType(type: string): CreationMode {
    if (type.includes("image")) return "image";
    if (type.includes("text")) return "text";
    return "video";
}

function supersededCreationTaskIds(tasks: readonly Pick<GenerationTask, "id" | "retryOf">[]) {
    const byId = new Map(tasks.map((task) => [task.id, task]));
    const superseded = new Set<string>();
    for (const task of tasks) {
        let cursor = task.retryOf?.trim();
        const seen = new Set<string>();
        while (cursor && !seen.has(cursor)) {
            superseded.add(cursor);
            seen.add(cursor);
            cursor = byId.get(cursor)?.retryOf?.trim();
        }
    }
    return superseded;
}

function promptForReattachedMessage(conversation: CreationConversation | undefined, prompt: string) {
    const listed = prompt.trim();
    if (!listed || !conversation) return listed;
    // 列表接口会截断提示词；只有本地已有更长、且以列表文本为前缀的用户消息时才沿用全文。
    const match = conversation.messages.find((message) => message.role === "user" && typeof message.content === "string" && message.content.startsWith(listed) && message.content.length >= listed.length);
    return match && typeof match.content === "string" && match.content.trim() ? match.content : listed;
}

function messagesForReattachedTask(conversation: CreationConversation | undefined, tasks: GenerationTask[]): CreationMessage[] {
    const first = tasks[0];
    const context = first.clientContext;
    if (!context?.messageId) return [];
    const createdAt = first.createdAt;
    const mode = creationModeFromTaskType(first.type);
    const prompt = promptForReattachedMessage(conversation, first.prompt || "");
    const user: CreationMessage = {
        id: `${context.messageId}:user`,
        role: "user",
        mode,
        content: prompt,
        createdAt,
        model: first.model,
    };
    const assistant: CreationMessage = {
        id: context.messageId,
        role: "assistant",
        mode,
        content: "",
        createdAt,
        status: "pending",
        taskIds: tasks.map((task) => task.id),
        model: first.model,
        generationStatus: first.status,
        generationStage: first.stage,
        generationMediaStage: first.mediaStage,
        generationProgress: typeof first.progress === "number" && Number.isFinite(first.progress) ? Math.round(first.progress) : undefined,
    };
    return [user, assistant];
}

function insertConversationMessages(conversation: CreationConversation, messages: CreationMessage[]) {
    const next = [...conversation.messages];
    for (let index = 0; index < messages.length; index += 2) {
        const pair = messages.slice(index, index + 2);
        const createdAt = conversationTimestamp(pair[0]?.createdAt || pair[1]?.createdAt || "");
        let insertAt = next.length;
        for (let cursor = 0; cursor < next.length; cursor++) {
            if (conversationTimestamp(next[cursor].createdAt) > createdAt) {
                insertAt = cursor;
                break;
            }
        }
        next.splice(insertAt, 0, ...pair);
    }
    const latest = messages.reduce((stamp, message) => (conversationTimestamp(message.createdAt) > conversationTimestamp(stamp) ? message.createdAt : stamp), conversation.updatedAt);
    return { ...conversation, messages: next, updatedAt: latest };
}

/**
 * 创作页对话只存在浏览器本地。任务若已提交到服务端，但对应镜头消息从本地记录里丢失，
 * 按任务上的 conversationId / messageId 把用户消息和助手消息补回时间线。
 * 不创建、不重试计费任务；被后续重试取代的失败尝试不再单独补成镜头。
 */
export function reattachMissingCreationTaskMessages(conversations: CreationConversation[], tasks: GenerationTask[]): CreationConversation[] {
    const attachable = tasks.filter((task) => Boolean(task.clientContext?.conversationId?.trim() && task.clientContext?.messageId?.trim()));
    if (!attachable.length) return conversations;

    const superseded = supersededCreationTaskIds(attachable);
    const existingMessageIds = new Set<string>();
    const existingTaskIds = new Set<string>();
    for (const conversation of conversations) {
        for (const message of conversation.messages) {
            existingMessageIds.add(message.id);
            for (const taskId of message.taskIds || []) existingTaskIds.add(taskId);
        }
    }

    const groups = new Map<string, GenerationTask[]>();
    for (const task of attachable) {
        if (superseded.has(task.id)) continue;
        const conversationId = task.clientContext!.conversationId!.trim();
        const messageId = task.clientContext!.messageId!.trim();
        if (existingMessageIds.has(messageId) || existingTaskIds.has(task.id)) continue;
        const key = `${conversationId}\u001f${messageId}`;
        const group = groups.get(key) || [];
        group.push(task);
        groups.set(key, group);
    }
    if (!groups.size) return conversations;

    const next = conversations.map((conversation) => ({ ...conversation, messages: [...conversation.messages] }));
    const byId = new Map(next.map((conversation) => [conversation.id, conversation]));
    const grouped = Array.from(groups.entries()).sort((left, right) => {
        const leftTask = left[1][0];
        const rightTask = right[1][0];
        return conversationTimestamp(leftTask.createdAt) - conversationTimestamp(rightTask.createdAt) || leftTask.id.localeCompare(rightTask.id);
    });

    for (const [, group] of grouped) {
        const conversationId = group[0].clientContext!.conversationId!.trim();
        const sorted = [...group].sort((left, right) => (left.clientContext?.batchIndex || 0) - (right.clientContext?.batchIndex || 0) || conversationTimestamp(left.createdAt) - conversationTimestamp(right.createdAt));
        const existing = byId.get(conversationId);
        const messages = messagesForReattachedTask(existing, sorted);
        if (!messages.length) continue;
        if (existing) {
            const updated = insertConversationMessages(existing, messages);
            const index = next.findIndex((conversation) => conversation.id === conversationId);
            next[index] = updated;
            byId.set(conversationId, updated);
            continue;
        }
        const created = {
            id: conversationId,
            title: (messages[0]?.content || "").trim().slice(0, 24) || conversationId,
            messages,
            updatedAt: messages[messages.length - 1]?.createdAt || sorted[0].updatedAt || sorted[0].createdAt,
        } satisfies CreationConversation;
        next.unshift(created);
        byId.set(conversationId, created);
    }
    return next;
}
