export type SupervisorDecision = {
  action: string;
  memberID?: string;
  instruction?: string;
  expectedOutcome?: string;
  answer?: string;
};

export type SupervisorDecisionParseResult = {
  decision: SupervisorDecision | null;
  hasStructuredCandidate: boolean;
};

function toDecision(value: unknown): SupervisorDecision | null {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    return null;
  }
  const record = value as Record<string, unknown>;
  if (typeof record.action !== "string" || record.action.trim() === "") {
    return null;
  }
  const decision: SupervisorDecision = { action: record.action.trim() };
  for (const key of ["memberID", "instruction", "expectedOutcome", "answer"] as const) {
    const field = record[key];
    if (typeof field === "string" && field.trim() !== "") {
      decision[key] = field.trim();
    }
  }
  return decision;
}

type JSONCandidateResult = {
  candidates: string[];
  hasIncompleteObject: boolean;
};

function balancedJSONCandidates(text: string): JSONCandidateResult {
  const candidates: string[] = [];
  let start = -1;
  let depth = 0;
  let inString = false;
  let escaped = false;

  for (let index = 0; index < text.length; index += 1) {
    const character = text[index];
    if (inString) {
      if (escaped) {
        escaped = false;
      } else if (character === "\\") {
        escaped = true;
      } else if (character === '"') {
        inString = false;
      }
      continue;
    }
    if (character === '"') {
      inString = true;
      continue;
    }
    if (character === "{" && depth === 0) {
      start = index;
      depth = 1;
      continue;
    }
    if (character === "{" && depth > 0) {
      depth += 1;
      continue;
    }
    if (character === "}" && depth > 0) {
      depth -= 1;
      if (depth === 0 && start >= 0) {
        candidates.push(text.slice(start, index + 1));
        start = -1;
      }
    }
  }
  return { candidates, hasIncompleteObject: depth > 0 };
}

export function parseSupervisorDecision(raw: string): SupervisorDecisionParseResult {
  const text = raw.trim();
  if (!text) {
    return { decision: null, hasStructuredCandidate: false };
  }

  const hasStructuredCandidate =
    text.startsWith("{") ||
    /^```(?:json)?\b/i.test(text) ||
    /["']action["']\s*:/i.test(text);
  const { candidates, hasIncompleteObject } = balancedJSONCandidates(text);
  let decision: SupervisorDecision | null = null;
  for (const candidate of candidates) {
    try {
      decision = toDecision(JSON.parse(candidate)) ?? decision;
    } catch {
      // Keep looking for a later valid correction round.
    }
  }
  return {
    decision,
    hasStructuredCandidate: hasStructuredCandidate && !decision && hasIncompleteObject,
  };
}
