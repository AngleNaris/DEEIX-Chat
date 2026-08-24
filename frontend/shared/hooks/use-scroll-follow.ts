"use client";

import * as React from "react";

// FOLLOW_EDGE_THRESHOLD_PX 贴近底部阈值：滚动位置距底部小于该值时视为"跟随底部"。
// 与聊天页 MessageScroller 的 following-bottom 行为保持一致。
const FOLLOW_EDGE_THRESHOLD_PX = 16;
/**
 * useAutoScrollFollow 内部滚动容器"跟随最新内容"：
 * 内容变化（contentKey 变化）时，若用户仍贴近底部则自动滚到底部；
 * 用户向上滚动查看历史时暂停跟随，滚回底部后恢复。
 * 适用于流式渲染的思考内容、工具调用等滚动区域。
 *
 * ResizeObserver 负责异步 Markdown、代码高亮和媒体加载造成的尺寸变化，
 * MutationObserver 负责流式文本与节点变化。所有滚动写入统一延迟到下一帧，
 * 用户上滚后立即暂停，回到底部时恢复。
 */
export function useAutoScrollFollow<T extends HTMLElement>(contentKey: unknown) {
  const ref = React.useRef<T | null>(null);
  const followRef = React.useRef(true);
  const frameRef = React.useRef(0);

  const scrollToEnd = React.useCallback(() => {
    if (frameRef.current !== 0) {
      return;
    }
    frameRef.current = window.requestAnimationFrame(() => {
      frameRef.current = 0;
      const element = ref.current;
      if (element && followRef.current) {
        element.scrollTop = element.scrollHeight;
      }
    });
  }, []);

  const handleScroll = React.useCallback(() => {
    const element = ref.current;
    if (!element) return;
    const shouldFollow =
      element.scrollTop + element.clientHeight >=
      element.scrollHeight - FOLLOW_EDGE_THRESHOLD_PX;
    const resumed = shouldFollow && !followRef.current;
    followRef.current = shouldFollow;
    if (resumed) {
      scrollToEnd();
    }
  }, [scrollToEnd]);

  React.useLayoutEffect(() => {
    const element = ref.current;
    if (!element || !followRef.current) return;
    element.scrollTop = element.scrollHeight;
    scrollToEnd();
  }, [contentKey, scrollToEnd]);

  React.useEffect(() => {
    const element = ref.current;
    if (!element) return;

    const resizeObserver =
      typeof ResizeObserver === "undefined"
        ? null
        : new ResizeObserver(() => {
            scrollToEnd();
          });
    const observeSizes = () => {
      if (!resizeObserver) return;
      resizeObserver.disconnect();
      resizeObserver.observe(element);
      for (const child of element.children) {
        resizeObserver.observe(child);
      }
    };
    observeSizes();

    const mutationObserver =
      typeof MutationObserver === "undefined"
        ? null
        : new MutationObserver(() => {
            observeSizes();
            scrollToEnd();
          });
    mutationObserver?.observe(element, {
      childList: true,
      subtree: true,
      characterData: true,
    });

    return () => {
      resizeObserver?.disconnect();
      mutationObserver?.disconnect();
      if (frameRef.current !== 0) {
        window.cancelAnimationFrame(frameRef.current);
        frameRef.current = 0;
      }
    };
  }, [scrollToEnd]);

  return { ref, onScroll: handleScroll };
}
