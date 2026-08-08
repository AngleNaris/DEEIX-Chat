"use client";

import * as React from "react";

import { getUserSettings, patchUserSettings } from "@/shared/api/user-settings";
import { useAuthSession } from "@/shared/auth/auth-session-context";

const TIMEZONE_SETTING_KEY = "timezone";

function detectBrowserTimeZone(): string | null {
  try {
    const timeZone = Intl.DateTimeFormat().resolvedOptions().timeZone;
    return timeZone && timeZone.length > 0 ? timeZone : null;
  } catch {
    return null;
  }
}

/**
 * 登录后将浏览器时区（IANA 名称）静默同步到用户设置。
 * 仅当设置中缺失或与浏览器时区不一致时才写入，保证系统提示词时间按用户本地时区渲染。
 */
export function UserTimeZoneSync() {
  const { accessToken } = useAuthSession();
  const syncedRef = React.useRef(false);

  React.useEffect(() => {
    if (!accessToken || syncedRef.current) {
      return;
    }
    const browserTimeZone = detectBrowserTimeZone();
    if (!browserTimeZone) {
      return;
    }

    syncedRef.current = true;
    void (async () => {
      try {
        const settings = await getUserSettings(accessToken);
        if (settings[TIMEZONE_SETTING_KEY] === browserTimeZone) {
          return;
        }
        await patchUserSettings(accessToken, { [TIMEZONE_SETTING_KEY]: browserTimeZone });
      } catch {
        // 静默失败：不打断用户，下次会话重试。
      }
    })();
  }, [accessToken]);

  return null;
}
