export type AgentGroupFeatureStatus = "loading" | "enabled" | "disabled" | "error";

export type AgentGroupFeatureAccess = {
  allowListRequests: boolean;
  redirectDirectRoute: boolean;
  showNavigation: boolean;
};

export function resolveAgentGroupFeatureAccess(
  status: AgentGroupFeatureStatus,
): AgentGroupFeatureAccess {
  const enabled = status === "enabled";
  return {
    allowListRequests: enabled,
    redirectDirectRoute: status === "disabled" || status === "error",
    showNavigation: enabled,
  };
}

export function filterAgentGroupNavigationItems<T extends { id: string }>(
  items: readonly T[],
  showAgentGroups: boolean,
): T[] {
  return showAgentGroups ? [...items] : items.filter((item) => item.id !== "agentGroups");
}
