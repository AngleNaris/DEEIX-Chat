export function shouldStartGenerationResume({
  conversationID,
  pendingRunID,
  generationActive,
  generationFailed,
}: {
  conversationID: string | null | undefined;
  pendingRunID: string | null | undefined;
  generationActive: boolean;
  generationFailed: boolean;
}): boolean {
  return Boolean(
    conversationID?.trim() &&
      pendingRunID?.trim() &&
      !generationActive &&
      !generationFailed,
  );
}
