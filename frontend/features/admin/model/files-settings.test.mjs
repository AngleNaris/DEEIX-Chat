import assert from "node:assert/strict";
import test from "node:test";

import { OCR_ENGINES, resolveOCREngine, resolveVisibleFields, SETTINGS_GROUPS } from "./files-settings.ts";

test("system vision remains a selectable image OCR engine", () => {
  const group = SETTINGS_GROUPS.find((item) => item.key === "extraction");
  const engine = group?.fields.find((field) => field.namespace === "extract" && field.key === "ocr_engine");

  assert.equal(resolveOCREngine("system_vision"), OCR_ENGINES.SYSTEM_VISION);
  assert.equal(resolveOCREngine("unknown"), OCR_ENGINES.RAPIDOCR);
  assert.ok(engine?.type === "select" && engine.options?.some((option) => option.value === OCR_ENGINES.SYSTEM_VISION));
  assert.equal(
    resolveVisibleFields(group, { "extract.image_ocr_enabled": "true" }).some((field) => field.key === "ocr_engine"),
    true,
  );
});
