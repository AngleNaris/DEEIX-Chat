"use client";

import * as React from "react";

import { ChevronDown } from "@/components/animate-ui/icons/chevron-down";
import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from "@/components/ui/accordion";
import { Marker, MarkerContent } from "@/components/ui/marker";
import type { ChatTraceBlock, ChatTraceEvent } from "@/features/chat/types/messages";
import { useProcessTraceLabels } from "@/features/chat/hooks/use-process-trace-labels";
import {
  hasActiveToolTraceCalls,
  MessageToolChainTrace,
} from "@/features/chat/components/message/message-tool-trace";
import { StreamdownRender } from "@/shared/components/markdown/streamdown-render";
import { cn } from "@/lib/utils";
import { TRACE_ROOT_CLASS } from "@/features/chat/components/shared/message-process-trace-shared";
import type { TraceDisplayEvent } from "@/features/chat/model/message-process-trace";

function traceEventToBlock(event: ChatTraceEvent): ChatTraceBlock {
  return {
    title: event.title,
    summary: event.summary,
    contentMarkdown: event.contentMarkdown,
    status: event.status,
    stage: event.stage,
    roundID: event.roundID,
    parentEventID: event.parentEventID,
    updatedAt: event.updatedAt,
    payloadJson: event.payloadJson,
  };
}

function isToolTraceEvent(event: ChatTraceEvent): boolean {
  if (event.stage === "think" || event.phase === "upstream_think" || event.eventType === "think") {
    return false;
  }
  return event.stage === "tool" || event.phase === "tools" || event.eventType === "tool";
}

function isThinkTraceEvent(event: ChatTraceEvent): boolean {
  return event.stage === "think" || event.phase === "upstream_think" || event.eventType === "think";
}

function buildTraceDisplayEvents(events: ChatTraceEvent[]): TraceDisplayEvent[] {
  return events
    .filter((event) => isToolTraceEvent(event) || isThinkTraceEvent(event))
    .sort((left, right) => left.seq - right.seq)
    .map((event) => {
      if (isThinkTraceEvent(event)) {
        return { event, kind: "think" };
      }
      return { event, kind: "tool" };
    });
}

function traceBlockDisplayText(block: Pick<ChatTraceBlock, "contentMarkdown" | "summary">): string {
  return block.contentMarkdown?.trim() || block.summary?.trim() || "";
}

type OrderedThinkBlock = ChatTraceBlock & {
  seq: number;
};

function mergeThinkTraceBlock(events: TraceDisplayEvent[], activeThinkBlock?: ChatTraceBlock): ChatTraceBlock | undefined {
  const blocks: OrderedThinkBlock[] = events
    .filter((item) => item.kind === "think")
    .map((item) => ({ ...traceEventToBlock(item.event), seq: item.event.seq }));

  if (activeThinkBlock) {
    const activeText = traceBlockDisplayText(activeThinkBlock);
    const activeIndex = blocks.findIndex((block) => {
      const sameRound = Boolean(activeThinkBlock.roundID && block.roundID === activeThinkBlock.roundID);
      const sameParent = Boolean(activeThinkBlock.parentEventID && block.parentEventID === activeThinkBlock.parentEventID);
      const sameText = Boolean(activeText && traceBlockDisplayText(block) === activeText);
      return sameRound || sameParent || sameText;
    });
    if (activeIndex >= 0) {
      blocks[activeIndex] = { ...activeThinkBlock, seq: blocks[activeIndex].seq };
    } else {
      blocks.push({ ...activeThinkBlock, seq: Number.MAX_SAFE_INTEGER });
    }
  }

  if (blocks.length === 0) {
    return undefined;
  }

  const ordered = [...blocks].sort((left, right) => left.seq - right.seq);
  const parts: string[] = [];
  for (const block of ordered) {
    const text = traceBlockDisplayText(block);
    if (text && !parts.includes(text)) {
      parts.push(text);
    }
  }
  if (parts.length === 0) {
    return undefined;
  }

  const latest = ordered[ordered.length - 1];
  return {
    ...latest,
    stage: "think",
    status: ordered.some((block) => block.status === "streaming") ? "streaming" : latest.status || "completed",
    contentMarkdown: parts.join("\n\n"),
    contentSegments: parts,
  };
}

function splitTraceDisplayEvents(events: TraceDisplayEvent[], activeThinkBlock?: ChatTraceBlock) {
  return {
    toolEvents: events.filter((item) => item.kind === "tool"),
    thinkBlock: mergeThinkTraceBlock(events, activeThinkBlock),
  };
}

function firstTraceSeq(events: TraceDisplayEvent[], kind: TraceDisplayEvent["kind"]): number | undefined {
  const matched = events.filter((item) => item.kind === kind).map((item) => item.event.seq);
  if (matched.length === 0) {
    return undefined;
  }
  return Math.min(...matched);
}

export function MessageTraceEventBlocks({
  events: traceEvents,
  activeToolBlock,
  activeThinkBlock,
  messageStreaming,
  autoCollapseReady,
}: {
  events: ChatTraceEvent[];
  activeToolBlock?: ChatTraceBlock;
  activeThinkBlock?: ChatTraceBlock;
  messageStreaming?: boolean;
  autoCollapseReady?: boolean;
}) {
  const displayEvents = React.useMemo(() => buildTraceDisplayEvents(traceEvents), [traceEvents]);
  const { toolEvents, thinkBlock } = React.useMemo(
    () => splitTraceDisplayEvents(displayEvents, activeThinkBlock),
    [activeThinkBlock, displayEvents],
  );
  if (toolEvents.length === 0 && !activeToolBlock && !thinkBlock) {
    return null;
  }

  const toolSeq = firstTraceSeq(displayEvents, "tool");
  const thinkSeq = firstTraceSeq(displayEvents, "think");
  const shouldRenderThinkFirst = Boolean(
    thinkBlock &&
      (
        (toolEvents.length === 0 && !activeToolBlock) ||
        thinkSeq === undefined ||
        toolSeq === undefined ||
        thinkSeq <= toolSeq
      ),
  );
  const toolTrace = (
    <MessageToolChainTrace
      events={toolEvents}
      activeToolBlock={activeToolBlock}
      streaming={Boolean(
        messageStreaming &&
        (
          activeToolBlock?.status === "streaming" ||
          hasActiveToolTraceCalls(activeToolBlock?.payloadJson) ||
          toolEvents.some((item) => item.event.status === "streaming" || hasActiveToolTraceCalls(item.event.payloadJson))
        ),
      )}
      autoCollapseReady={autoCollapseReady || Boolean(thinkBlock)}
    />
  );
  const thinkTrace = thinkBlock ? (
    <MessageUpstreamThink
      block={thinkBlock}
      streaming={Boolean(messageStreaming && thinkBlock.status === "streaming")}
      autoCollapseReady={autoCollapseReady}
    />
  ) : null;

  return (
    <>
      {shouldRenderThinkFirst ? thinkTrace : toolTrace}
      {shouldRenderThinkFirst ? toolTrace : thinkTrace}
    </>
  );
}


export function MessageUpstreamThink({
  block,
  streaming,
  autoCollapseReady,
  title,
  subtitle,
}: {
  block?: ChatTraceBlock;
  streaming?: boolean;
  autoCollapseReady?: boolean;
  title?: string;
  subtitle?: string;
}) {
  const labels = useProcessTraceLabels();
  const [accordionValue, setAccordionValue] = React.useState(() => (streaming ? "upstream-think" : ""));
  const wasStreamingRef = React.useRef(Boolean(streaming));

  React.useEffect(() => {
    if (streaming) {
      setAccordionValue("upstream-think");
      wasStreamingRef.current = true;
      return;
    }

    if (wasStreamingRef.current && autoCollapseReady) {
      setAccordionValue("");
    }
    if (autoCollapseReady) {
      wasStreamingRef.current = false;
    }
  }, [autoCollapseReady, streaming]);

  if (!block) {
    return null;
  }

  const open = accordionValue === "upstream-think";
  const resolvedTitle = title ?? (streaming ? labels.think.titleActive : labels.think.titleDone);
  const resolvedSubtitle = subtitle ?? (streaming ? labels.think.subtitleActive : labels.think.subtitleDone);
  const contentSegments = block.contentSegments?.filter((item) => item.trim()) ?? [];

  return (
    <div className={TRACE_ROOT_CLASS}>
      <Accordion
        type="single"
        collapsible
        value={accordionValue}
        onValueChange={(value) => setAccordionValue(value || "")}
        className="w-full"
      >
        <AccordionItem value="upstream-think" className="border-b-0">
          <AccordionTrigger
            iconPosition="none"
            className="group items-start justify-between gap-1.5 py-0 text-left no-underline hover:no-underline"
          >
            <div className="min-w-0 flex-1">
              <div className="flex items-center gap-1.5">
                <Marker
                  render={<span />}
                  className={cn(
                    "inline-flex min-h-0 w-auto text-[13px] font-medium transition-colors",
                    !streaming && "text-muted-foreground group-hover:text-foreground",
                  )}
                >
                  <MarkerContent className={cn("min-w-0", streaming && "shimmer")}>
                    {resolvedTitle}
                  </MarkerContent>
                </Marker>
              </div>
              <div className="mt-0.5 truncate text-[11px] font-normal leading-4 text-muted-foreground/62">{resolvedSubtitle}</div>
            </div>
            <ChevronDown
              className={cn(
                "mt-0.5 size-3.5 shrink-0 text-muted-foreground transition-transform duration-200 group-hover:text-foreground",
                open && "rotate-180",
              )}
            />
          </AccordionTrigger>
          <AccordionContent className="px-0 pb-0 pt-1.5 duration-[350ms] ease-in-out">
            {/* 思维过程内容设最大高度并滚动，避免长思考将页面撑得过长 */}
            <div className="max-h-80 space-y-3 overflow-y-auto pr-1">
              {contentSegments.length > 0 ? (
                contentSegments.map((content, index) => (
                  <StreamdownRender
                    key={`${index}-${content.slice(0, 24)}`}
                    content={content}
                    streaming={Boolean(streaming && index === contentSegments.length - 1)}
                    variant="thinking"
                  />
                ))
              ) : (
                <StreamdownRender content={block.contentMarkdown} streaming={Boolean(streaming)} variant="thinking" />
              )}
            </div>
          </AccordionContent>
        </AccordionItem>
      </Accordion>
    </div>
  );
}
