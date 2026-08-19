import assert from "node:assert/strict";
import test from "node:test";

import {
  ConversationStreamDisconnectedError,
  readRecoverableSequencedJSONStream,
  readSequencedJSONStream,
} from "./conversation-stream-reader.ts";

const encoder = new TextEncoder();

function responseFromChunks(chunks) {
  return new Response(
    new ReadableStream({
      start(controller) {
        for (const chunk of chunks) {
          controller.enqueue(encoder.encode(chunk));
        }
        controller.close();
      },
    }),
    { status: 200 },
  );
}

function readerOptions(initialResponse, openRecovery, overrides = {}) {
  return {
    initialResponse,
    openRecovery,
    idleTimeoutMS: 0,
    parseEvent: JSON.parse,
    getEventSeq: (event) => event.seq,
    handleEvent: (event) => event.type === "completed" ? event.data : null,
    ...overrides,
  };
}

test("stream recovery resumes after the last accepted sequence and drops replayed events", async () => {
  const accepted = [];
  const recoveryAfter = [];
  const completed = await readRecoverableSequencedJSONStream(
    readerOptions(
      responseFromChunks(['{"type":"delta","seq":1,"delta":"A"}\n']),
      async (afterSeq) => {
        recoveryAfter.push(afterSeq);
        return responseFromChunks([
          '{"type":"delta","seq":1,"delta":"A"}\n',
          '{"type":"delta","seq":2,"delta":"B"}\n',
          '{"type":"completed","seq":3,"data":{"content":"AB"}}\n',
        ]);
      },
      {
        handleEvent: (event) => {
          accepted.push(event.seq);
          return event.type === "completed" ? event.data : null;
        },
      },
    ),
  );

  assert.deepEqual(recoveryAfter, [1]);
  assert.deepEqual(accepted, [1, 2, 3]);
  assert.deepEqual(completed, { content: "AB" });
});

test("an idle stream is canceled and recovered without consuming the rapid reconnect budget", async () => {
  let canceled = false;
  const stalledResponse = new Response(
    new ReadableStream({
      cancel() {
        canceled = true;
      },
    }),
    { status: 200 },
  );

  const completed = await readRecoverableSequencedJSONStream(
    readerOptions(
      stalledResponse,
      async (afterSeq) => {
        assert.equal(afterSeq, 0);
        return responseFromChunks([
          '{"type":"completed","seq":1,"data":{"status":"done"}}\n',
        ]);
      },
      { idleTimeoutMS: 10 },
    ),
  );

  assert.equal(canceled, true);
  assert.deepEqual(completed, { status: "done" });
});

test("an abort immediately cancels a pending read and releases the response body", async () => {
  let canceled = false;
  const controller = new AbortController();
  const response = new Response(
    new ReadableStream({
      cancel() {
        canceled = true;
      },
    }),
    { status: 200 },
  );

  const reading = readSequencedJSONStream(
    response,
    {
      signal: controller.signal,
      idleTimeoutMS: 0,
      parseEvent: JSON.parse,
      getEventSeq: (event) => event.seq,
      handleEvent: () => null,
    },
  );
  controller.abort();

  await assert.rejects(reading, (error) => error instanceof DOMException && error.name === "AbortError");
  assert.equal(canceled, true);
  assert.equal(response.body.locked, false);
});

test("a completed read releases the response body", async () => {
  const response = responseFromChunks([
    '{"type":"completed","seq":1,"data":{"status":"done"}}\n',
  ]);

  const result = await readSequencedJSONStream(response, {
    parseEvent: JSON.parse,
    getEventSeq: (event) => event.seq,
    handleEvent: (event) => event.type === "completed" ? event.data : null,
  });

  assert.deepEqual(result.result, { status: "done" });
  assert.equal(response.body.locked, false);
});

test("repeated empty recovery streams fail instead of looping forever", async () => {
  let recoveryCalls = 0;
  await assert.rejects(
    readRecoverableSequencedJSONStream(
      readerOptions(
        responseFromChunks([]),
        async () => {
          recoveryCalls += 1;
          return responseFromChunks([]);
        },
        { maxRapidReconnects: 2 },
      ),
    ),
    ConversationStreamDisconnectedError,
  );
  assert.equal(recoveryCalls, 2);
});
