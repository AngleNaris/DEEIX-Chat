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
 */
export function useAutoScrollFollow<T extends HTMLElement>(contentKey: unknown) {
  const ref = React.useRef<T | null>(null);
  const followRef = React.useRef(true);

  const handleScroll = React.useCallback(() => {
    const element = ref.current;
    if (!element) return;
    followRef.current =
      element.scrollTop + element.clientHeight >=
      element.scrollHeight - FOLLOW_EDGE_THRESHOLD_PX;
  }, []);

  React.useEffect(() => {
    const element = ref.current;
    if (!element || !followRef.current) return;
    element.scrollTop = element.scrollHeight;
  }, [contentKey]);

  return { ref, onScroll: handleScroll };
}
