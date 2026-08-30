import {
  Bot,
  Brain,
  BriefcaseBusiness,
  Code2,
  type LucideIcon,
  Music2,
  Palette,
  ShieldCheck,
  Sparkles,
  Star,
  Users,
  Wrench,
} from "lucide-react";
import * as React from "react";

const ROLE_ICON_MAP: Record<string, LucideIcon> = {
  bot: Bot,
  brain: Brain,
  briefcase: BriefcaseBusiness,
  "briefcase-business": BriefcaseBusiness,
  code: Code2,
  "code-2": Code2,
  music: Music2,
  "music-2": Music2,
  palette: Palette,
  "shield-check": ShieldCheck,
  sparkles: Sparkles,
  star: Star,
  users: Users,
  wrench: Wrench,
};

const ROLE_ICON_NAME_RE = /^[a-z0-9]+(?:-[a-z0-9]+)*$/i;

export function RoleIcon({
  className,
  strokeWidth = 1.8,
  value,
}: {
  className?: string;
  strokeWidth?: number;
  value: string;
}) {
  const normalized = value.trim().toLowerCase();
  const Icon = ROLE_ICON_MAP[normalized];
  if (Icon) {
    return React.createElement(Icon, { "aria-hidden": true, className, strokeWidth });
  }
  if (!normalized || ROLE_ICON_NAME_RE.test(normalized)) {
    return React.createElement(Sparkles, { "aria-hidden": true, className, strokeWidth });
  }
  return React.createElement("span", { "aria-hidden": true, className }, value);
}
