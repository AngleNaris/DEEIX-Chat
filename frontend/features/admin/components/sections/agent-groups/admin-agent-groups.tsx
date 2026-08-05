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

const AGENT_GROUP_NAMESPACE = "agent_group";

const AGENT_GROUP_NUMBER_FIELDS = [
  { key: "max_steps_per_run", field: "maxStepsPerRun", min: 1 },
  { key: "max_attempts_per_step", field: "maxAttemptsPerStep", min: 1 },
  { key: "attempt_lease_seconds", field: "attemptLeaseSeconds", min: 1 },
] as const;

function toSettingsMap(items: { key: string; value: string }[]): Record<string, string> {
  const result: Record<string, string> = {};
  for (const item of items) {
    result[item.key] = item.value;
  }
  return result;
}

export function AdminAgentGroupsSettingsPage() {
  const t = useTranslations("adminAgentGroups");
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
      const items = await listAdminSettingsByNamespace(token, AGENT_GROUP_NAMESPACE);
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
    for (const key of ["enabled", ...AGENT_GROUP_NUMBER_FIELDS.map((item) => item.key)]) {
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
        namespace: AGENT_GROUP_NAMESPACE,
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
      const next = toSettingsMap(grouped[AGENT_GROUP_NAMESPACE] ?? []);
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
        </SettingsFieldList>
      </SettingsSection>

      <SettingsSectionSeparator />

      <SettingsSection title={t("runtime.title")}>
        <SettingsFieldList>
          {AGENT_GROUP_NUMBER_FIELDS.map((item, index) => {
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
