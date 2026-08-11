"use client";

import * as React from "react";

import type { ChatModelOption } from "@/features/chat/types/chat-runtime";
import { ModelIcon } from "@/shared/components/model-icon";
import { resolveModelIconURL, resolveModelIdentity } from "@/shared/lib/model-identity";
import { cn } from "@/lib/utils";

export function ChatModelIdentity({
  model,
  density = "default",
}: {
  model: ChatModelOption;
  density?: "default" | "compact";
}) {
  const platformModelName = model.platformModelName.trim();
  const identity = React.useMemo(
    () =>
      resolveModelIdentity({
        code: model.platformModelName,
        vendor: model.vendor,
        icon: model.icon,
      }),
    [model.icon, model.platformModelName, model.vendor],
  );
  const iconURL = React.useMemo(() => resolveModelIconURL(identity.modelIcon), [identity.modelIcon]);
  const compact = density === "compact";

  return (
    <div className={cn("flex min-w-0 items-center", compact ? "gap-2" : "gap-2.5")}>
      <ModelIcon iconUrl={iconURL} label={platformModelName} />
      <div className="min-w-0 flex-1 overflow-hidden">
        <div className={cn("flex items-center", compact ? "gap-1" : "gap-1.5")}>
          <p
            className={cn(
              "truncate font-medium text-foreground",
              compact ? "text-[12.5px] leading-4" : "text-[13px] leading-4.5",
            )}
          >
            {platformModelName}
          </p>
        </div>
      </div>
    </div>
  );
}
