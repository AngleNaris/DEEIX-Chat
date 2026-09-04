export type GroupRunRecoveryState = {
  groupRunID?: string | null;
  status?: string | null;
  steps?: readonly unknown[] | null;
  resuming?: boolean | null;
};

export function isEmptyResumingGroupRunPlaceholder(
  run: GroupRunRecoveryState | null | undefined,
): boolean {
  return Boolean(
    run?.resuming === true &&
      !(run.groupRunID ?? "").trim() &&
      (run.steps?.length ?? 0) === 0,
  );
}

export function shouldStartGroupRunDetailRecovery(
  run: GroupRunRecoveryState | null | undefined,
): boolean {
  return !run || isEmptyResumingGroupRunPlaceholder(run);
}

export function shouldImportGroupRunDetail(
  existing: GroupRunRecoveryState | null | undefined,
): boolean {
  if (!existing || isEmptyResumingGroupRunPlaceholder(existing)) {
    return true;
  }
  return existing.status !== "pending" && existing.status !== "running" && existing.resuming !== true;
}
