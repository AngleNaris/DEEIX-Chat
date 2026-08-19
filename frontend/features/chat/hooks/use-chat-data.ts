"use client";

import * as React from "react";
import { useTranslations } from "next-intl";

import { resolveAccessToken } from "@/shared/auth/resolve-access-token";
import { cancelMessageGeneration, listMessagesPage, resumeMessageGenerationStream } from "@/shared/api/conversation";
import {
  ensureLiveGroupRunPlaceholder,
  upsertGroupRunEvent,
  upsertLiveGroupRunThink,
  upsertLiveGroupRunTool,
} from "@/features/agent-groups/model/group-run-store";
import { buildMediaImagePreviewMarkdown } from "@/features/chat/model/media-image-preview";
import { shouldStartGenerationResume } from "@/features/chat/model/generation-stream-recovery";
import {
  clearLiveUpstreamThinkTrace,
  upsertLiveUpstreamThinkTrace,
} from "@/features/chat/model/upstream-think-store";
import { isGroupStreamAwareEvent } from "@/shared/api/conversation";
import type { MessageDTO } from "@/shared/api/conversation.types";

const MESSAGE_PAGE_SIZE = 100;

type ChatDataState = {
  loading: boolean;
  loadingOlder: boolean;
  errorMsg: string;
  messages: MessageDTO[];
  total: number;
  hasOlder: boolean;
};

type ActiveResumeStream = {
  controller: AbortController;
  runID: string;
  accessToken: string | null;
};

type ResumeTextReplayState = {
  baseContent: string;
  replayedContent: string;
  visibleContent: string;
};

function appendResumedTextDelta(state: ResumeTextReplayState, delta: string): string {
  if (!delta) {
    return state.visibleContent;
  }

  state.replayedContent += delta;
  const { baseContent, replayedContent } = state;
  if (!baseContent) {
    state.visibleContent = replayedContent;
    return state.visibleContent;
  }

  if (
    replayedContent === baseContent ||
    baseContent.startsWith(replayedContent) ||
    baseContent.includes(replayedContent)
  ) {
    state.visibleContent = baseContent;
    return state.visibleContent;
  }

  if (replayedContent.startsWith(baseContent)) {
    state.visibleContent = replayedContent;
    return state.visibleContent;
  }

  const maxOverlapLength = Math.min(baseContent.length, replayedContent.length);
  for (let length = maxOverlapLength; length > 0; length -= 1) {
    if (baseContent.endsWith(replayedContent.slice(0, length))) {
      state.visibleContent = `${baseContent}${replayedContent.slice(length)}`;
      return state.visibleContent;
    }
  }

  state.visibleContent = `${state.visibleContent}${delta}`;
  return state.visibleContent;
}

export function useChatData(
  conversationID: string | null,
  {
    activeGenerationRunsRef,
    failedGenerationRunsRef,
    isGroupConversation,
  }: {
    activeGenerationRunsRef?: React.RefObject<Set<string>>;
    failedGenerationRunsRef?: React.RefObject<Set<string>>;
    // 群组会话：刷新恢复时重建群组运行占位（§16.8 正在恢复运行）。
    isGroupConversation?: boolean;
  } = {},
) {
  const t = useTranslations("chat.data");
  const tSubmit = useTranslations("chat.submit");
  const [state, setState] = React.useState<ChatDataState>({
    loading: Boolean(conversationID),
    loadingOlder: false,
    errorMsg: "",
    messages: [],
    total: 0,
    hasOlder: false,
  });
  const [reloadToken, setReloadToken] = React.useState(0);
  const [resumingRunID, setResumingRunID] = React.useState("");
  const stateRef = React.useRef(state);
  stateRef.current = state;
  const previousConversationIDRef = React.useRef<string | null>(conversationID);
  const resumeSeqByRunRef = React.useRef<Record<string, number>>({});
  const pendingAssistantContentRef = React.useRef("");
  const resumeTextReplayByRunRef = React.useRef<Record<string, ResumeTextReplayState>>({});
  const activeResumeStreamRef = React.useRef<ActiveResumeStream | null>(null);
  const resumedRunIDsRef = React.useRef(new Set<string>());
  const isGroupConversationRef = React.useRef(Boolean(isGroupConversation));
  const tSubmitRef = React.useRef(tSubmit);
  isGroupConversationRef.current = Boolean(isGroupConversation);
  tSubmitRef.current = tSubmit;
  // 恢复游标只在对应的可见内容仍被保留时有效，两者必须同步清理。
  const clearResumeCheckpoint = React.useCallback((runID: string) => {
    const normalizedRunID = runID.trim();
    if (!normalizedRunID) {
      return;
    }
    delete resumeSeqByRunRef.current[normalizedRunID];
    delete resumeTextReplayByRunRef.current[normalizedRunID];
  }, []);

  React.useEffect(() => {
    let cancelled = false;

    async function load() {
      if (!conversationID) {
        setState({
          loading: false,
          loadingOlder: false,
          errorMsg: "",
          messages: [],
          total: 0,
          hasOlder: false,
        });
        return;
      }

      const isConversationSwitch = previousConversationIDRef.current !== conversationID;
      previousConversationIDRef.current = conversationID;
      setState((prev) => ({
        loading: isConversationSwitch || prev.messages.length === 0,
        loadingOlder: false,
        errorMsg: "",
        messages: isConversationSwitch ? [] : prev.messages,
        total: isConversationSwitch ? 0 : prev.total,
        hasOlder: isConversationSwitch ? false : prev.hasOlder,
      }));
      try {
        const token = await resolveAccessToken();
        if (!token) {
          if (!cancelled) {
            setState({
              loading: false,
              loadingOlder: false,
              errorMsg: t("signInRequired"),
              messages: [],
              total: 0,
              hasOlder: false,
            });
          }
          return;
        }

        const data = await listMessagesPage(token, conversationID, {
          page: 1,
          pageSize: MESSAGE_PAGE_SIZE,
          tail: true,
        });
        if (cancelled) {
          return;
        }

        setState((prev) => {
          const firstTailMessageID = data.results[0]?.id ?? 0;
          // 只有已加载过额外历史页时才保留旧区间，避免普通 reload 无限累积 tail 消息。
          const loadedOlderMessages =
            isConversationSwitch ||
            firstTailMessageID <= 0 ||
            prev.messages.length <= MESSAGE_PAGE_SIZE
              ? []
              : prev.messages.filter((message) => message.id < firstTailMessageID);
          const messages = [...loadedOlderMessages, ...data.results];
          return {
            loading: false,
            loadingOlder: false,
            errorMsg: "",
            messages,
            total: data.total,
            hasOlder: messages.length < data.total,
          };
        });
      } catch {
        if (!cancelled) {
          setState((prev) => ({
            ...prev,
            loading: false,
            loadingOlder: false,
            errorMsg: t("loadFailed"),
          }));
        }
      }
    }

    void load();
    return () => {
      cancelled = true;
    };
  }, [conversationID, reloadToken, t]);

  const reload = React.useCallback(() => {
    setReloadToken((prev) => prev + 1);
  }, []);

  const replaceMessage = React.useCallback((nextMessage: MessageDTO) => {
    setState((prev) => ({
      ...prev,
      messages: prev.messages.map((message) =>
        message.publicID === nextMessage.publicID ? nextMessage : message,
      ),
    }));
  }, []);

  const loadOlderMessages = React.useCallback(async () => {
    const current = stateRef.current;
    if (!conversationID || current.loading || current.loadingOlder || !current.hasOlder || current.messages.length === 0) {
      return false;
    }

    const beforeID = current.messages[0]?.id ?? 0;
    if (beforeID <= 0) {
      setState((prev) => {
        const next = { ...prev, hasOlder: false };
        stateRef.current = next;
        return next;
      });
      return false;
    }

    setState((prev) => {
      const next = { ...prev, loadingOlder: true };
      stateRef.current = next;
      return next;
    });
    try {
      const token = await resolveAccessToken();
      if (!token) {
        setState((prev) => {
          const next = { ...prev, loadingOlder: false, hasOlder: false };
          stateRef.current = next;
          return next;
        });
        return false;
      }

      const data = await listMessagesPage(token, conversationID, {
        pageSize: MESSAGE_PAGE_SIZE,
        beforeID,
      });
      if (previousConversationIDRef.current !== conversationID) {
        return false;
      }
      let loaded = false;
      setState((prev) => {
        const existingPublicIDs = new Set(prev.messages.map((message) => message.publicID));
        const olderMessages = data.results.filter((message) => !existingPublicIDs.has(message.publicID));
        const messages = [...olderMessages, ...prev.messages];
        loaded = olderMessages.length > 0;
        const next = {
          ...prev,
          loadingOlder: false,
          messages,
          total: data.total,
          hasOlder: loaded && messages.length < data.total,
        };
        stateRef.current = next;
        return next;
      });
      return loaded;
    } catch {
      setState((prev) => {
        const next = { ...prev, loadingOlder: false };
        stateRef.current = next;
        return next;
      });
      return false;
    }
  }, [conversationID]);

  const loadAllOlderMessages = React.useCallback(async ({ maxPages = 50 }: { maxPages?: number } = {}) => {
    for (let iteration = 0; iteration < maxPages; iteration += 1) {
      if (!stateRef.current.hasOlder) {
        return true;
      }
      const loaded = await loadOlderMessages();
      if (!loaded) {
        return !stateRef.current.hasOlder;
      }
    }
    return !stateRef.current.hasOlder;
  }, [loadOlderMessages]);

  const cancelResumedGeneration = React.useCallback(async () => {
    const active = activeResumeStreamRef.current;
    if (!active) {
      return false;
    }

    active.controller.abort();
    clearResumeCheckpoint(active.runID);
    setResumingRunID("");

    const token = active.accessToken ?? (await resolveAccessToken());
    if (!token) {
      return false;
    }

    const result = await cancelMessageGeneration(token, active.runID).catch(() => null);
    reload();
    return Boolean(result?.canceled);
  }, [clearResumeCheckpoint, reload]);

  const pendingAssistant = React.useMemo(() => {
    for (let index = state.messages.length - 1; index >= 0; index -= 1) {
      const message = state.messages[index];
      if (message.role === "assistant" && message.status === "pending") {
        return message;
      }
    }
    return null;
  }, [state.messages]);

  const pendingRunID = pendingAssistant?.runID?.trim() || "";

  React.useEffect(
    () => () => {
      for (const runID of resumedRunIDsRef.current) {
        clearLiveUpstreamThinkTrace(runID);
      }
      resumedRunIDsRef.current.clear();
    },
    [],
  );

  React.useEffect(() => {
    pendingAssistantContentRef.current = pendingAssistant?.content ?? "";
  }, [pendingAssistant?.content]);

  React.useEffect(() => {
    const shouldResume = shouldStartGenerationResume({
      conversationID,
      pendingRunID,
      generationActive: Boolean(activeGenerationRunsRef?.current.has(pendingRunID)),
      generationFailed: Boolean(failedGenerationRunsRef?.current.has(pendingRunID)),
    });
    if (!shouldResume) {
      setResumingRunID("");
      return;
    }

    const controller = new AbortController();
    let closed = false;
    const afterSeq = resumeSeqByRunRef.current[pendingRunID] ?? 0;
    const baseContent = pendingAssistantContentRef.current;
    const resumeTextReplayByRun = resumeTextReplayByRunRef.current;
    const clearResumeTextReplay = () => {
      delete resumeTextReplayByRun[pendingRunID];
    };
    const isResumeInactive = () => closed || controller.signal.aborted;
    const updateResumeState = (update: (current: ChatDataState) => ChatDataState) => {
      setState((current) => isResumeInactive() ? current : update(current));
    };
    resumeTextReplayByRun[pendingRunID] = {
      baseContent,
      replayedContent: afterSeq > 0 ? baseContent : "",
      visibleContent: baseContent,
    };
    activeResumeStreamRef.current = {
      controller,
      runID: pendingRunID,
      accessToken: null,
    };
    resumedRunIDsRef.current.add(pendingRunID);
    setResumingRunID(pendingRunID);

    async function resume() {
      try {
        const token = await resolveAccessToken();
        if (!token || controller.signal.aborted) {
          return;
        }
        if (activeResumeStreamRef.current?.controller === controller) {
          activeResumeStreamRef.current.accessToken = token;
        }
        const completed = await resumeMessageGenerationStream(token, pendingRunID, {
          signal: controller.signal,
          afterSeq,
          onEventSeq: (seq) => {
            if (isResumeInactive()) {
              return;
            }
            resumeSeqByRunRef.current[pendingRunID] = Math.max(resumeSeqByRunRef.current[pendingRunID] ?? 0, seq);
          },
          onMediaStatus: (event) => {
            const status = event.status.trim();
            const contentType = event.content_type === "video" ? "video" : "image";
            const activityLabel =
              status === "queued"
                ? tSubmitRef.current(contentType === "video" ? "mediaStatus.videoQueued" : "mediaStatus.queued")
                : status === "running"
                  ? tSubmitRef.current(contentType === "video" ? "mediaStatus.videoRunning" : "mediaStatus.running")
                  : status === "saving_artifact"
                    ? tSubmitRef.current(
                        contentType === "video" ? "mediaStatus.videoSavingArtifact" : "mediaStatus.savingArtifact",
                      )
                    : event.message.trim() || status;
            updateResumeState((prev) => ({
              ...prev,
              messages: prev.messages.map((message) =>
                message.runID === pendingRunID && message.role === "assistant" && message.status === "pending"
                  ? { ...message, activityLabel, contentType }
                  : message,
              ),
            }));
          },
          onMediaImageDelta: (event) => {
            if (isResumeInactive()) {
              return;
            }
            clearResumeTextReplay();
            const previewMarkdown = buildMediaImagePreviewMarkdown(event, tSubmitRef.current("imagePreviewAlt"));
            if (!previewMarkdown) {
              return;
            }
            updateResumeState((prev) => ({
              ...prev,
              messages: prev.messages.map((message) =>
                message.runID === pendingRunID && message.role === "assistant" && message.status === "pending"
                  ? { ...message, content: previewMarkdown, contentType: "image", activityLabel: "" }
                  : message,
              ),
            }));
          },
          onDelta: (delta) => {
            if (isResumeInactive()) {
              return;
            }
            let replayState = resumeTextReplayByRun[pendingRunID];
            if (!replayState) {
              replayState = {
                baseContent: "",
                replayedContent: "",
                visibleContent: "",
              };
              resumeTextReplayByRun[pendingRunID] = replayState;
            }
            const nextContent = appendResumedTextDelta(replayState, delta);
            updateResumeState((prev) => ({
              ...prev,
              messages: prev.messages.map((message) =>
                message.runID === pendingRunID && message.role === "assistant" && message.status === "pending"
                  ? { ...message, content: nextContent }
                  : message,
              ),
            }));
          },
          onProcessUpdate: (event) => {
            updateResumeState((prev) => ({
              ...prev,
              messages: prev.messages.map((message) =>
                message.runID === pendingRunID && message.role === "assistant" && message.status === "pending"
                  ? { ...message, processTrace: event.trace }
                  : message,
              ),
            }));
          },
          onUpstreamThinkDelta: (event) => {
            if (isResumeInactive()) {
              return;
            }
            if (isGroupStreamAwareEvent(event)) {
              upsertLiveGroupRunThink(pendingRunID, event);
            } else {
              upsertLiveUpstreamThinkTrace(pendingRunID, event);
            }
          },
          onGroupEvent: (event) => {
            if (isResumeInactive()) {
              return;
            }
            upsertGroupRunEvent(pendingRunID, event);
          },
          onToolEvent: (event) => {
            if (isResumeInactive()) {
              return;
            }
            // 群组内部 Actor 回合转发的工具调用（§15：tool_call/tool_result 带 Actor 元数据）。
            if (isGroupStreamAwareEvent(event)) {
              upsertLiveGroupRunTool(pendingRunID, event);
            }
          },
          onUsage: (event) => {
            updateResumeState((prev) => ({
              ...prev,
              messages: prev.messages.map((message) =>
                message.runID === pendingRunID && message.role === "assistant" && message.status === "pending"
                  ? {
                      ...message,
                      inputTokens: event.input_tokens > 0 ? event.input_tokens : message.inputTokens,
                      outputTokens: event.output_tokens > 0 ? event.output_tokens : message.outputTokens,
                      cacheReadTokens:
                        event.cache_read_tokens > 0 ? event.cache_read_tokens : message.cacheReadTokens,
                      cacheWriteTokens:
                        event.cache_write_tokens > 0 ? event.cache_write_tokens : message.cacheWriteTokens,
                      reasoningTokens:
                        event.reasoning_tokens > 0 ? event.reasoning_tokens : message.reasoningTokens,
                    }
                  : message,
              ),
            }));
          },
        });
        if (!controller.signal.aborted && completed === null) {
          reload();
        }
        if (!controller.signal.aborted && completed) {
          clearResumeCheckpoint(pendingRunID);
          reload();
        }
      } catch (error) {
        if (!controller.signal.aborted && error instanceof Error && error.name !== "AbortError") {
          setResumingRunID("");
          reload();
        }
      } finally {
        if (activeResumeStreamRef.current?.controller === controller) {
          activeResumeStreamRef.current = null;
        }
        if (!controller.signal.aborted && !closed) {
          setResumingRunID("");
        }
      }
    }

    // §16.8：群组会话恢复时先创建占位运行（正在恢复运行），事件重放到达后回填时间线。
    if (isGroupConversationRef.current) {
      ensureLiveGroupRunPlaceholder(pendingRunID, { resuming: true });
    }
    void resume();
    return () => {
      closed = true;
      controller.abort();
      if (activeResumeStreamRef.current?.controller === controller) {
        activeResumeStreamRef.current = null;
      }
    };
  }, [
    activeGenerationRunsRef,
    clearResumeCheckpoint,
    conversationID,
    failedGenerationRunsRef,
    pendingRunID,
    reload,
  ]);

  React.useEffect(() => {
    if (
      !conversationID ||
      !pendingAssistant ||
      activeGenerationRunsRef?.current.has(pendingRunID) ||
      failedGenerationRunsRef?.current.has(pendingRunID) ||
      (pendingRunID && pendingRunID === resumingRunID)
    ) {
      return;
    }
    const timer = window.setTimeout(() => {
      reload();
    }, 1500);
    return () => {
      window.clearTimeout(timer);
    };
  }, [activeGenerationRunsRef, conversationID, failedGenerationRunsRef, pendingAssistant, pendingRunID, reload, resumingRunID]);

  return {
    ...state,
    cancelResumedGeneration,
    loadOlderMessages,
    loadAllOlderMessages,
    reload,
    replaceMessage,
    resumingRunID,
  };
}
