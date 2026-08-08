"use client";

import * as React from "react";
import { Save } from "lucide-react";
import { useTranslations } from "next-intl";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import { resolveAccessToken } from "@/shared/auth/resolve-access-token";
import {
  SettingsFieldItem,
  SettingsFieldList,
  SettingsFieldRow,
  SettingsPage,
  SettingsSection,
  SettingsSectionSeparator,
} from "@/shared/components/settings-layout";
import {
  listAdminSettingsByNamespace,
  patchAdminSettings,
} from "@/features/admin/api";
import { resolveAdminErrorMessage } from "@/features/admin/utils/admin-error";
import type { PatchSettingItem } from "@/shared/api/settings.types";

const PLATFORM_TOOLS_NAMESPACE = "platform_tools";

const PLATFORM_TOOLS_NUMBER_FIELDS = [
  { key: "file_reindex_delay_seconds", field: "fileReindexDelaySeconds", min: 1 },
] as const;

function toSettingsMap(items: { key: string; value: string }[]): Record<string, string> {
  const result: Record<string, string> = {};
  for (const item of items) {
    result[item.key] = item.value;
  }
  return result;
}

/**
 * AdminPlatformToolsSettingsPage 管理平台 Agent 工具开关：
 * enabled 控制只读工具注入，write_enabled 控制写工具注入，
 * file_reindex_delay_seconds 控制 write_file 后提取/RAG 重建的缓冲秒数。
 */
export function AdminPlatformToolsSettingsPage() {
  const t = useTranslations("adminPlatformTools");
  const [values, setValues] = React.useState<Record<string, string>>({});
  const [saved, setSaved] = React.useState<Record<string, string>>({});
  const [loading, setLoading] = React.useState(true);
  const [saving, setSaving] = React.useState(false);

  const loadSettings = React.useCallback(async () => {
    try {
      const token = await resolveAccessToken();
      if (!token) {
        toast.error(t("toast.sessionExpired"), { description: t("toast.signInAgain") });
        return;
      }
      const items = await listAdminSettingsByNamespace(token, PLATFORM_TOOLS_NAMESPACE);
      const next = toSettingsMap(items);
      setValues(next);
      setSaved(next);
    } catch (error) {
      toast.error(t("toast.loadFailed"), { description: resolveAdminErrorMessage(error) });
    } finally {
      setLoading(false);
    }
  }, [t]);

  React.useEffect(() => {
    void loadSettings();
  }, [loadSettings]);

  const dirty = React.useMemo(() => {
    const result = new Set<string>();
    for (const key of ["enabled", "write_enabled", ...PLATFORM_TOOLS_NUMBER_FIELDS.map((item) => item.key)]) {
      if ((values[key] ?? "") !== (saved[key] ?? "")) {
        result.add(key);
      }
    }
    return result;
  }, [saved, values]);

  const handleSave = React.useCallback(async () => {
    const items: PatchSettingItem[] = dirty.size === 0
      ? []
      : [...dirty].map((key) => ({
        namespace: PLATFORM_TOOLS_NAMESPACE,
        key,
        value: values[key] ?? "",
      }));
    if (items.length === 0) {
      return;
    }
    setSaving(true);
    try {
      const token = await resolveAccessToken();
      if (!token) {
        toast.error(t("toast.sessionExpired"), { description: t("toast.signInAgain") });
        return;
      }
      const grouped = await patchAdminSettings(token, { items });
      const next = toSettingsMap(grouped[PLATFORM_TOOLS_NAMESPACE] ?? []);
      setValues(next);
      setSaved(next);
      toast.success(t("toast.updated"));
    } catch (error) {
      toast.error(t("toast.saveFailed"), { description: resolveAdminErrorMessage(error) });
    } finally {
      setSaving(false);
    }
  }, [dirty, t, values]);

  if (loading) {
    return (
      <SettingsPage>
        <SettingsSection title={t("feature.title")}>
          <SettingsFieldList>
            <SettingsFieldItem>
              <SettingsFieldRow title={t("fields.enabled.label")}>
                <Switch checked={false} disabled />
              </SettingsFieldRow>
            </SettingsFieldItem>
          </SettingsFieldList>
        </SettingsSection>
      </SettingsPage>
    );
  }

  return (
    <SettingsPage>
      <SettingsSection title={t("feature.title")}>
        <p className="mb-3 text-xs leading-5 text-muted-foreground">{t("feature.description")}</p>
        <SettingsFieldList>
          <SettingsFieldItem>
            <SettingsFieldRow
              title={t("fields.enabled.label")}
              description={t("fields.enabled.description")}
            >
              <Switch
                checked={values.enabled === "true"}
                onCheckedChange={(checked) => {
                  setValues((prev) => ({ ...prev, enabled: checked ? "true" : "false" }));
                }}
                disabled={saving}
              />
            </SettingsFieldRow>
          </SettingsFieldItem>
          <SettingsFieldItem index={1}>
            <SettingsFieldRow
              title={t("fields.writeEnabled.label")}
              description={t("fields.writeEnabled.description")}
            >
              <Switch
                checked={values.write_enabled === "true"}
                onCheckedChange={(checked) => {
                  setValues((prev) => ({ ...prev, write_enabled: checked ? "true" : "false" }));
                }}
                disabled={saving}
              />
            </SettingsFieldRow>
          </SettingsFieldItem>
        </SettingsFieldList>
      </SettingsSection>

      <SettingsSectionSeparator />

      <SettingsSection title={t("runtime.title")}>
        <p className="mb-3 text-xs leading-5 text-muted-foreground">{t("runtime.description")}</p>
        <SettingsFieldList>
          {PLATFORM_TOOLS_NUMBER_FIELDS.map((item, index) => {
            const numericValue = values[item.key] ?? "";
            return (
              <SettingsFieldItem key={item.key} index={index}>
                <SettingsFieldRow
                  title={t(`fields.${item.field}.label`)}
                  description={t(`fields.${item.field}.description`)}
                >
                  <Input
                    type="number"
                    min={item.min}
                    step={1}
                    value={numericValue}
                    className="h-8 w-36 text-xs"
                    onChange={(event) => {
                      const raw = event.target.value.replace(/[^0-9]/g, "");
                      setValues((prev) => ({ ...prev, [item.key]: raw }));
                    }}
                    disabled={saving}
                  />
                </SettingsFieldRow>
              </SettingsFieldItem>
            );
          })}
        </SettingsFieldList>
      </SettingsSection>

      <div className="flex justify-end">
        <Button
          variant="default"
          size="sm"
          disabled={saving || dirty.size === 0}
          onClick={() => void handleSave()}
        >
          <Save className="size-3.5" />
          {t("actions.save")}
        </Button>
      </div>
    </SettingsPage>
  );
}
