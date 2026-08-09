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

  const snapshot = await requestArtifactSnapshot(frame);
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
}
