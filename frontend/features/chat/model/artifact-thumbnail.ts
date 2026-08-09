"use client";

import { domToWebp } from "modern-screenshot";

import {
  ARTIFACT_THUMBNAIL_REQUEST,
  ARTIFACT_THUMBNAIL_RESPONSE,
} from "@/features/chat/model/artifact-thumbnail-protocol";

const CAPTURE_WIDTH = 960;
const CAPTURE_HEIGHT = 240;
const CAPTURE_TIMEOUT_MS = 5_000;
const MAX_SNAPSHOT_HTML_LENGTH = 2 * 1024 * 1024;
const MAX_THUMBNAIL_DATA_URL_LENGTH = 512 * 1024;

type ArtifactThumbnailSnapshot = {
  html: string;
  backgroundColor: string;
};

type ArtifactThumbnailResponse = {
  type: typeof ARTIFACT_THUMBNAIL_RESPONSE;
  requestId: string;
  html?: string;
  backgroundColor?: string;
  error?: string;
};

function createRequestID(): string {
  if (typeof crypto !== "undefined" && typeof crypto.randomUUID === "function") {
    return crypto.randomUUID();
  }
  return `${Date.now()}-${Math.random().toString(36).slice(2)}`;
}

function isThumbnailResponse(value: unknown): value is ArtifactThumbnailResponse {
  if (!value || typeof value !== "object") {
    return false;
  }
  const response = value as Partial<ArtifactThumbnailResponse>;
  return (
    response.type === ARTIFACT_THUMBNAIL_RESPONSE &&
    typeof response.requestId === "string"
  );
}

// waitForFrameLoad 等待预览 iframe 完成加载后再发起快照请求。
// 快照脚本以同步脚本形式位于文档 head 中，load 事件触发时脚本必然已执行，
// 因此等待 load 即可保证消息监听器就绪（复杂制品渲染慢、主题样式触发
// iframe 重载时，避免请求发出后无人响应导致超时）。
function waitForFrameLoad(frame: HTMLIFrameElement, timeoutMs: number): Promise<void> {
  return new Promise((resolve) => {
    const finish = () => {
      frame.removeEventListener("load", onLoad);
      window.clearTimeout(timer);
      resolve();
    };
    const onLoad = () => finish();
    const timer = window.setTimeout(finish, timeoutMs);
    frame.addEventListener("load", onLoad);
  });
}

function requestArtifactSnapshot(frame: HTMLIFrameElement): Promise<ArtifactThumbnailSnapshot> {
  const frameWindow = frame.contentWindow;
  if (!frameWindow) {
    return Promise.reject(new Error("Artifact preview is not ready"));
  }

  const requestId = createRequestID();
  return new Promise((resolve, reject) => {
    const timeout = window.setTimeout(() => {
      cleanup();
      reject(new Error("Artifact thumbnail snapshot timed out"));
    }, CAPTURE_TIMEOUT_MS);

    const cleanup = () => {
      window.clearTimeout(timeout);
      window.removeEventListener("message", handleMessage);
    };

    const handleMessage = (event: MessageEvent<unknown>) => {
      if (event.source !== frameWindow || !isThumbnailResponse(event.data)) {
        return;
      }
      if (event.data.requestId !== requestId) {
        return;
      }

      cleanup();
      if (event.data.error) {
        reject(new Error(event.data.error));
        return;
      }
      if (!event.data.html || event.data.html.length > MAX_SNAPSHOT_HTML_LENGTH) {
        reject(new Error("Artifact thumbnail snapshot is invalid"));
        return;
      }
      resolve({
        html: event.data.html,
        backgroundColor: event.data.backgroundColor || "#ffffff",
      });
    };

    window.addEventListener("message", handleMessage);
    frameWindow.postMessage(
      {
        type: ARTIFACT_THUMBNAIL_REQUEST,
        requestId,
      },
      "*",
    );
  });
}

async function requestArtifactSnapshotWhenReady(
  frame: HTMLIFrameElement,
): Promise<ArtifactThumbnailSnapshot> {
  await waitForFrameLoad(frame, CAPTURE_TIMEOUT_MS);
  return requestArtifactSnapshot(frame);
}

function loadSnapshotFrame(frame: HTMLIFrameElement, html: string): Promise<void> {
  return new Promise((resolve, reject) => {
    const timeout = window.setTimeout(() => {
      cleanup();
      reject(new Error("Artifact thumbnail render timed out"));
    }, CAPTURE_TIMEOUT_MS);

    const cleanup = () => {
      window.clearTimeout(timeout);
      frame.removeEventListener("load", handleLoad);
    };
    const handleLoad = () => {
      cleanup();
      resolve();
    };

    frame.addEventListener("load", handleLoad);
    frame.srcdoc = html;
  });
}

async function renderSnapshot(
  snapshot: ArtifactThumbnailSnapshot,
  scale: number,
  quality: number,
): Promise<string> {
  const frame = document.createElement("iframe");
  frame.setAttribute("aria-hidden", "true");
  frame.setAttribute("sandbox", "allow-same-origin");
  frame.referrerPolicy = "no-referrer";
  frame.tabIndex = -1;
  Object.assign(frame.style, {
    position: "fixed",
    left: "-10000px",
    top: "0",
    width: `${CAPTURE_WIDTH}px`,
    height: `${CAPTURE_HEIGHT}px`,
    border: "0",
    clipPath: "inset(50%)",
    pointerEvents: "none",
  });
  document.body.appendChild(frame);

  try {
    await loadSnapshotFrame(frame, snapshot.html);
    const frameDocument = frame.contentDocument;
    if (!frameDocument) {
      throw new Error("Artifact thumbnail document is unavailable");
    }
    await Promise.race([
      frameDocument.fonts?.ready ?? Promise.resolve(),
      new Promise<void>((resolve) => window.setTimeout(resolve, 500)),
    ]);

    return await domToWebp(frameDocument.documentElement, {
      width: CAPTURE_WIDTH,
      height: CAPTURE_HEIGHT,
      scale,
      quality,
      backgroundColor: snapshot.backgroundColor,
      timeout: CAPTURE_TIMEOUT_MS,
      font: false,
      features: {
        copyScrollbar: false,
        restoreScrollPosition: false,
      },
      style: {
        width: `${CAPTURE_WIDTH}px`,
        height: `${CAPTURE_HEIGHT}px`,
        margin: "0",
        overflow: "hidden",
      },
    });
  } finally {
    frame.remove();
  }
}

function isSupportedThumbnail(value: string): boolean {
  return (
    value.startsWith("data:image/webp;base64,") ||
    value.startsWith("data:image/png;base64,") ||
    value.startsWith("data:image/jpeg;base64,")
  );
}

export async function captureArtifactPreviewThumbnail(
  frame: HTMLIFrameElement | null,
): Promise<string | null> {
  if (!frame || typeof window === "undefined" || !document.body) {
    return null;
  }

  // 复杂制品渲染慢 / 主题样式触发 iframe 重载时首次请求可能超时，重试一次兜底。
  for (let attempt = 0; attempt < 2; attempt += 1) {
    try {
      const snapshot = await requestArtifactSnapshotWhenReady(frame);
      let thumbnail = await renderSnapshot(snapshot, 0.5, 0.76);
      if (thumbnail.length > MAX_THUMBNAIL_DATA_URL_LENGTH) {
        thumbnail = await renderSnapshot(snapshot, 0.4, 0.62);
      }
      if (
        thumbnail.length > MAX_THUMBNAIL_DATA_URL_LENGTH ||
        !isSupportedThumbnail(thumbnail)
      ) {
        return null;
      }
      return thumbnail;
    } catch (error) {
      if (attempt === 1) {
        throw error;
      }
    }
  }
  return null;
}
