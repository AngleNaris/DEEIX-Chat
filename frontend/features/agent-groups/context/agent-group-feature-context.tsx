"use client";

import * as React from "react";

import { resolveAgentGroupFeatureAccess } from "@/features/agent-groups/model/agent-group-feature";
import type { AgentGroupFeatureStatus } from "@/features/agent-groups/model/agent-group-feature";
import { getAgentGroupFeature } from "@/shared/api/agent-groups";
import { useAuthSession } from "@/shared/auth/auth-session-context";

type AgentGroupFeatureContextValue = {
  enabled: boolean;
  refresh: () => Promise<void>;
  status: AgentGroupFeatureStatus;
};

const DEFAULT_VALUE: AgentGroupFeatureContextValue = {
  enabled: false,
  refresh: async () => undefined,
  status: "loading",
};

const AgentGroupFeatureContext = React.createContext<AgentGroupFeatureContextValue>(DEFAULT_VALUE);

export function AgentGroupFeatureProvider({ children }: { children: React.ReactNode }) {
  const { accessToken } = useAuthSession();
  const [status, setStatus] = React.useState<AgentGroupFeatureStatus>("loading");
  const requestRevisionRef = React.useRef(0);

  const refresh = React.useCallback(async () => {
    const requestRevision = ++requestRevisionRef.current;
    try {
      const feature = await getAgentGroupFeature(accessToken);
      if (requestRevision === requestRevisionRef.current) {
        setStatus(feature.enabled ? "enabled" : "disabled");
      }
    } catch {
      if (requestRevision === requestRevisionRef.current) {
        setStatus("error");
      }
    }
  }, [accessToken]);

  React.useEffect(() => {
    setStatus("loading");
    void refresh();
    return () => {
      requestRevisionRef.current += 1;
    };
  }, [refresh]);

  React.useEffect(() => {
    const handleFocus = () => {
      void refresh();
    };
    window.addEventListener("focus", handleFocus);
    return () => window.removeEventListener("focus", handleFocus);
  }, [refresh]);

  const access = resolveAgentGroupFeatureAccess(status);
  const value = React.useMemo<AgentGroupFeatureContextValue>(
    () => ({ enabled: access.allowListRequests, refresh, status }),
    [access.allowListRequests, refresh, status],
  );

  return (
    <AgentGroupFeatureContext.Provider value={value}>
      {children}
    </AgentGroupFeatureContext.Provider>
  );
}

export function useAgentGroupFeature() {
  return React.useContext(AgentGroupFeatureContext);
}
