export const DEFAULT_CONVERSATION_STREAM_IDLE_TIMEOUT_MS = 45_000;
export const DEFAULT_CONVERSATION_STREAM_MAX_RAPID_RECONNECTS = 5;
// 空闲断连的独立上限：连接从未产出事件即 idle 超时视为无进展，
// 避免永久空闲流被「idle 清零计数」逻辑无限重连（约 45s×6 ≈ 4.5 分钟封顶）。
export const DEFAULT_CONVERSATION_STREAM_MAX_IDLE_RECONNECTS = 5;

export type ConversationStreamSequenceCursor = {
  lastSeq: number;
};

export class ConversationStreamTransportError extends Error {
  override cause?: unknown;

  constructor(message: string, cause?: unknown) {
    super(message);
    this.name = "ConversationStreamTransportError";
    this.cause = cause;
  }
}

export class ConversationStreamIdleError extends ConversationStreamTransportError {
  constructor() {
    super("conversation stream became idle");
    this.name = "ConversationStreamIdleError";
  }
}

export class ConversationStreamDisconnectedError extends ConversationStreamTransportError {
  constructor(cause?: unknown) {
    super("conversation stream disconnected repeatedly", cause);
    this.name = "ConversationStreamDisconnectedError";
  }
}

export function normalizeConversationStreamSequence(value: unknown): number {
  return typeof value === "number" && Number.isFinite(value) && value > 0 ? Math.floor(value) : 0;
}

export function createConversationStreamSequenceCursor(initialSeq: unknown = 0): ConversationStreamSequenceCursor {
  return {
    lastSeq: normalizeConversationStreamSequence(initialSeq),
  };
}

export function acceptConversationStreamSequence(
  cursor: ConversationStreamSequenceCursor,
  value: unknown,
): boolean {
  const seq = normalizeConversationStreamSequence(value);
  if (seq === 0) {
    return true;
  }
  if (seq <= cursor.lastSeq) {
    return false;
  }
  cursor.lastSeq = seq;
  return true;
}

type SequencedJSONStreamOptions<TEvent, TResult> = {
  signal?: AbortSignal;
  afterSeq?: number;
  idleTimeoutMS?: number;
  parseEvent: (source: string) => TEvent;
  getEventSeq: (event: TEvent) => unknown;
  handleEvent: (event: TEvent, responseStatus: number) => TResult | null;
  onEventSeq?: (seq: number) => void;
};

type RecoverableSequencedJSONStreamOptions<TEvent, TResult> =
  SequencedJSONStreamOptions<TEvent, TResult> & {
    initialResponse: Response;
    openRecovery: (afterSeq: number, signal?: AbortSignal) => Promise<Response>;
    shouldRetryOpenError?: (error: unknown) => boolean;
    maxRapidReconnects?: number;
    maxIdleReconnects?: number;
  };

type SequencedJSONStreamResult<TResult> = {
  result: TResult | null;
  lastSeq: number;
};

function abortError(): DOMException {
  return new DOMException("Aborted", "AbortError");
}

function isAbortError(error: unknown): boolean {
  return error instanceof DOMException && error.name === "AbortError";
}

function extractJSONDocuments(source: string): { documents: string[]; remainder: string } {
  const documents: string[] = [];
  let startIndex = -1;
  let depth = 0;
  let inString = false;
  let escaped = false;
  let lastConsumedIndex = 0;

  for (let index = 0; index < source.length; index += 1) {
    const char = source[index];

    if (startIndex < 0) {
      if (char === "{") {
        startIndex = index;
        depth = 1;
        lastConsumedIndex = index;
      } else if (!/\s/.test(char)) {
        break;
      } else {
        lastConsumedIndex = index + 1;
      }
      continue;
    }

    if (inString) {
      if (escaped) {
        escaped = false;
      } else if (char === "\\") {
        escaped = true;
      } else if (char === "\"") {
        inString = false;
      }
      continue;
    }

    if (char === "\"") {
      inString = true;
      continue;
    }

    if (char === "{") {
      depth += 1;
      continue;
    }

    if (char !== "}") {
      continue;
    }

    depth -= 1;
    if (depth !== 0) {
      continue;
    }

    documents.push(source.slice(startIndex, index + 1));
    startIndex = -1;
    lastConsumedIndex = index + 1;
  }

  if (startIndex >= 0) {
    return {
      documents,
      remainder: source.slice(startIndex),
    };
  }

  return {
    documents,
    remainder: source.slice(lastConsumedIndex),
  };
}

async function readChunk(
  reader: ReadableStreamDefaultReader<Uint8Array>,
  signal: AbortSignal | undefined,
  idleTimeoutMS: number,
): Promise<ReadableStreamReadResult<Uint8Array>> {
  if (signal?.aborted) {
    throw abortError();
  }

  let timeoutID: ReturnType<typeof setTimeout> | undefined;
  let onAbort: (() => void) | undefined;
  const interrupted = new Promise<never>((_, reject) => {
    if (signal) {
      onAbort = () => reject(abortError());
      signal.addEventListener("abort", onAbort, { once: true });
    }
    if (idleTimeoutMS > 0) {
      timeoutID = setTimeout(() => reject(new ConversationStreamIdleError()), idleTimeoutMS);
    }
  });

  try {
    return await Promise.race([reader.read(), interrupted]);
  } catch (error) {
    if (signal?.aborted || isAbortError(error)) {
      await reader.cancel().catch(() => undefined);
      throw abortError();
    }
    if (error instanceof ConversationStreamIdleError) {
      await reader.cancel().catch(() => undefined);
      throw error;
    }
    if (error instanceof ConversationStreamTransportError) {
      throw error;
    }
    throw new ConversationStreamTransportError("conversation stream read failed", error);
  } finally {
    if (timeoutID !== undefined) {
      clearTimeout(timeoutID);
    }
    if (onAbort) {
      signal?.removeEventListener("abort", onAbort);
    }
  }
}

export async function readSequencedJSONStream<TEvent, TResult>(
  response: Response,
  options: SequencedJSONStreamOptions<TEvent, TResult>,
): Promise<SequencedJSONStreamResult<TResult>> {
  const sequenceCursor = createConversationStreamSequenceCursor(options.afterSeq);
  if (!response.body) {
    return { result: null, lastSeq: sequenceCursor.lastSeq };
  }

  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";
  let result: TResult | null = null;
  let reachedEOF = false;

  const consumeEvent = (event: TEvent) => {
    const rawSeq = options.getEventSeq(event);
    if (!acceptConversationStreamSequence(sequenceCursor, rawSeq)) {
      return;
    }
    const seq = normalizeConversationStreamSequence(rawSeq);
    if (seq > 0) {
      options.onEventSeq?.(seq);
    }
    const nextResult = options.handleEvent(event, response.status);
    if (nextResult !== null) {
      result = nextResult;
    }
  };
  const consumeDocument = (document: string) => {
    consumeEvent(options.parseEvent(document));
  };

  try {
    while (true) {
      const { done, value } = await readChunk(
        reader,
        options.signal,
        options.idleTimeoutMS ?? 0,
      );
      buffer += decoder.decode(value ?? new Uint8Array(), { stream: !done });

      const { documents, remainder } = extractJSONDocuments(buffer);
      buffer = remainder;
      for (const document of documents) {
        consumeDocument(document);
      }

      if (done) {
        reachedEOF = true;
        break;
      }
    }

    const tail = buffer.trim();
    if (tail) {
      let event: TEvent;
      try {
        event = options.parseEvent(tail);
      } catch (error) {
        throw new ConversationStreamTransportError("conversation stream ended with a partial event", error);
      }
      consumeEvent(event);
    }

    return { result, lastSeq: sequenceCursor.lastSeq };
  } finally {
    if (!reachedEOF) {
      await reader.cancel().catch(() => undefined);
    }
    reader.releaseLock();
  }
}

function reconnectDelayMS(rapidReconnects: number): number {
  if (rapidReconnects <= 0) {
    return 0;
  }
  return Math.min(250 * (2 ** (rapidReconnects - 1)), 2_000);
}

async function waitForReconnect(delayMS: number, signal?: AbortSignal): Promise<void> {
  if (signal?.aborted) {
    throw abortError();
  }
  if (delayMS <= 0) {
    return;
  }

  await new Promise<void>((resolve, reject) => {
    const onAbort = () => {
      clearTimeout(timeoutID);
      signal?.removeEventListener("abort", onAbort);
      reject(abortError());
    };
    const timeoutID = setTimeout(() => {
      signal?.removeEventListener("abort", onAbort);
      resolve();
    }, delayMS);
    signal?.addEventListener("abort", onAbort, { once: true });
  });
}

export async function readRecoverableSequencedJSONStream<TEvent, TResult>(
  options: RecoverableSequencedJSONStreamOptions<TEvent, TResult>,
): Promise<TResult> {
  let response = options.initialResponse;
  let lastSeq = normalizeConversationStreamSequence(options.afterSeq);
  let rapidReconnects = 0;
  let rapidIdleStreak = 0;
  const maxRapidReconnects =
    options.maxRapidReconnects ?? DEFAULT_CONVERSATION_STREAM_MAX_RAPID_RECONNECTS;
  const maxIdleReconnects =
    options.maxIdleReconnects ?? DEFAULT_CONVERSATION_STREAM_MAX_IDLE_RECONNECTS;

  while (true) {
    const beforeReadSeq = lastSeq;
    let readError: unknown;
    try {
      const readResult = await readSequencedJSONStream(response, {
        ...options,
        afterSeq: lastSeq,
        onEventSeq: (seq) => {
          lastSeq = Math.max(lastSeq, seq);
          options.onEventSeq?.(seq);
        },
      });
      lastSeq = Math.max(lastSeq, readResult.lastSeq);
      if (readResult.result !== null) {
        return readResult.result;
      }
    } catch (error) {
      if (options.signal?.aborted || isAbortError(error)) {
        throw abortError();
      }
      if (!(error instanceof ConversationStreamTransportError)) {
        throw error;
      }
      readError = error;
    }

    const madeProgress = lastSeq > beforeReadSeq;
    if (madeProgress) {
      // 真实推进（含 idle 前已产出事件）：两类计数都视为健康。
      rapidReconnects = 0;
      rapidIdleStreak = 0;
    } else if (readError instanceof ConversationStreamIdleError) {
      // idle 且本次连接从未产出事件：与无进展传输错误同等对待。
      // 用独立预算限制，避免永久空闲流被无限重连。
      rapidIdleStreak += 1;
      if (rapidIdleStreak > maxIdleReconnects) {
        throw new ConversationStreamDisconnectedError(readError);
      }
    } else {
      rapidReconnects += 1;
    }
    if (rapidReconnects > maxRapidReconnects) {
      throw new ConversationStreamDisconnectedError(readError);
    }

    await waitForReconnect(reconnectDelayMS(rapidReconnects), options.signal);
    while (true) {
      try {
        response = await options.openRecovery(lastSeq, options.signal);
        break;
      } catch (error) {
        if (options.signal?.aborted || isAbortError(error)) {
          throw abortError();
        }
        if (!options.shouldRetryOpenError?.(error)) {
          throw error;
        }
        rapidReconnects += 1;
        if (rapidReconnects > maxRapidReconnects) {
          throw new ConversationStreamDisconnectedError(error);
        }
        await waitForReconnect(reconnectDelayMS(rapidReconnects), options.signal);
      }
    }
  }
}
