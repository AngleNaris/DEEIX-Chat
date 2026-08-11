"use client";

import * as React from "react";

// FOLLOW_EDGE_THRESHOLD_PX 贴近底部阈值：滚动位置距底部小于该值时视为"跟随底部"。
// 与聊天页 MessageScroller 的 following-bottom 行为保持一致。
const FOLLOW_EDGE_THRESHOLD_PX = 16;
// FOLLOW_STABLE_FRAMES 内容高度连续 N 帧不变后停止跟随循环（流式渲染完成后自动停止）。
const FOLLOW_STABLE_FRAMES = 3;

/**
 * useAutoScrollFollow 内部滚动容器"跟随最新内容"：
 * 内容变化（contentKey 变化）时，若用户仍贴近底部则自动滚到底部；
 * 用户向上滚动查看历史时暂停跟随，滚回底部后恢复。
 * 适用于流式渲染的思考内容、工具调用等滚动区域。
 *
 * 实现说明：contentKey 变化后先用 useLayoutEffect 同步滚底（浏览器绘制前，
 * 避免流式更新时"先看到未跟随、再跳底"的闪烁）；随后启动 rAF 跟随循环——
 * 流式渲染（markdown 解析、代码高亮、插件异步加载等）可能在 effect 之后继续
 * 增长 DOM，单次滚动赋值会停在中间；持续跟随数帧直到内容高度稳定，保证始终
 * 看到最新内容。用户上滚后（onScroll 置 followRef=false）循环立即停止。
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

  React.useLayoutEffect(() => {
    const element = ref.current;
    if (!element || !followRef.current) return;
    element.scrollTop = element.scrollHeight;

    let stableFrames = 0;
    let lastHeight = element.scrollHeight;
    let frameID = 0;
    const tick = () => {
      frameID = 0;
      const current = ref.current;
      if (!current || !followRef.current) {
        return;
      }
      const height = current.scrollHeight;
      if (height === lastHeight) {
        stableFrames += 1;
        if (stableFrames >= FOLLOW_STABLE_FRAMES) {
          return;
        }
      } else {
        stableFrames = 0;
        lastHeight = height;
      }
      current.scrollTop = current.scrollHeight;
      frameID = window.requestAnimationFrame(tick);
    };
    frameID = window.requestAnimationFrame(tick);
    return () => {
      if (frameID !== 0) {
        window.cancelAnimationFrame(frameID);
      }
    };
  }, [contentKey]);

  return { ref, onScroll: handleScroll };
}
