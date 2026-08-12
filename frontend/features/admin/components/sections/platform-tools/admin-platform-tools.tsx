"use client";

import * as React from "react";
import { ArrowDown, ArrowUp, Plus, Save, Trash2 } from "lucide-react";
import { useTranslations } from "next-intl";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
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
import { listPublicModels } from "@/shared/api/model";
import type { PublicModelDTO } from "@/shared/api/model.types";
import { parseKindsJSON } from "@/shared/model/llm-schema";
import type { PatchSettingItem } from "@/shared/api/settings.types";

const PLATFORM_TOOLS_NAMESPACE = "platform_tools";

const PLATFORM_TOOLS_NUMBER_FIELDS = [
  { key: "file_reindex_delay_seconds", field: "fileReindexDelaySeconds", min: 1 },
] as const;

const IMAGE_GEN_ENABLED_KEY = "image_gen_enabled";
const IMAGE_GEN_CHANNELS_KEY = "image_gen_channels";
const IMAGE_GEN_KIND = "image_gen";

type ImageGenChannel = {
  model: string;
  note: string;
};

function toSettingsMap(items: { key: string; value: string }[]): Record<string, string> {
  const result: Record<string, string> = {};
  for (const item of items) {
    result[item.key] = item.value;
  }
  return result;
}

function parseChannelsJSON(raw: string | undefined): ImageGenChannel[] {
  const normalized = (raw ?? "").trim();
  if (!normalized) {
    return [];
  }
  try {
    const parsed = JSON.parse(normalized) as unknown;
    if (!Array.isArray(parsed)) {
      return [];
    }
    return parsed.flatMap((item): ImageGenChannel[] => {
      if (item === null || typeof item !== "object" || Array.isArray(item)) {
        return [];
      }
      const source = item as Record<string, unknown>;
      const model = typeof source.model === "string" ? source.model.trim() : "";
      const note = typeof source.note === "string" ? source.note.trim() : "";
      if (!model) {
        return [];
      }
      return [{ model, note }];
    });
  } catch {
    return [];
  }
}

function stringifyChannelsJSON(channels: ImageGenChannel[]): string {
  return JSON.stringify(channels);
}

function channelsEqual(left: ImageGenChannel[], right: ImageGenChannel[]): boolean {
  if (left.length !== right.length) {
    return false;
  }
  return left.every((item, index) => item.model === right[index].model && item.note === right[index].note);
}

/**
 * AdminPlatformToolsSettingsPage 管理平台 Agent 工具开关：
 * enabled 控制只读工具注入，write_enabled 控制写工具注入，
 * file_reindex_delay_seconds 控制 write_file 后提取/RAG 重建的缓冲秒数，
 * image_gen_enabled + image_gen_channels 控制 image_gen 图片生成工具及其渠道。
 */
export function AdminPlatformToolsSettingsPage() {
  const t = useTranslations("adminPlatformTools");
  const [values, setValues] = React.useState<Record<string, string>>({});
  const [saved, setSaved] = React.useState<Record<string, string>>({});
  const [channels, setChannels] = React.useState<ImageGenChannel[]>([]);
  const [savedChannels, setSavedChannels] = React.useState<ImageGenChannel[]>([]);
  const [imageGenModels, setImageGenModels] = React.useState<PublicModelDTO[]>([]);
  const [modelsLoading, setModelsLoading] = React.useState(true);
  const [loading, setLoading] = React.useState(true);
  const [saving, setSaving] = React.useState(false);

  const loadImageGenModels = React.useCallback(async () => {
    try {
      const token = await resolveAccessToken();
      if (!token) {
        return;
      }
      const models = await listPublicModels(token);
      setImageGenModels(models.filter((item) => parseKindsJSON(item.kindsJSON).includes(IMAGE_GEN_KIND)));
    } catch {
      toast.error(t("imageGen.loadModelsFailed"));
    } finally {
      setModelsLoading(false);
    }
  }, [t]);

  const loadSettings = React.useCallback(async () => {
    try {
      const token = await resolveAccessToken();
      if (!token) {
        toast.error(t("toast.sessionExpired"), { description: t("toast.signInAgain") });
        return;
      }
      const [items] = await Promise.all([listAdminSettingsByNamespace(token, PLATFORM_TOOLS_NAMESPACE)]);
      const next = toSettingsMap(items);
      setValues(next);
      setSaved(next);
      setChannels(parseChannelsJSON(next[IMAGE_GEN_CHANNELS_KEY]));
      setSavedChannels(parseChannelsJSON(next[IMAGE_GEN_CHANNELS_KEY]));
    } catch (error) {
      toast.error(t("toast.loadFailed"), { description: resolveAdminErrorMessage(error) });
    } finally {
      setLoading(false);
    }
  }, [t]);

  React.useEffect(() => {
    void loadSettings();
    void loadImageGenModels();
  }, [loadSettings, loadImageGenModels]);

  const dirty = React.useMemo(() => {
    const result = new Set<string>();
    for (const key of ["enabled", "write_enabled", IMAGE_GEN_ENABLED_KEY, ...PLATFORM_TOOLS_NUMBER_FIELDS.map((item) => item.key)]) {
      if ((values[key] ?? "") !== (saved[key] ?? "")) {
        result.add(key);
      }
    }
    if (!channelsEqual(channels, savedChannels)) {
      result.add(IMAGE_GEN_CHANNELS_KEY);
    }
    return result;
  }, [channels, saved, savedChannels, values]);

  const handleSave = React.useCallback(async () => {
    const items: PatchSettingItem[] = dirty.size === 0
      ? []
      : [...dirty].map((key) => ({
        namespace: PLATFORM_TOOLS_NAMESPACE,
        key,
        value: key === IMAGE_GEN_CHANNELS_KEY
          ? stringifyChannelsJSON(channels)
          : (values[key] ?? ""),
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
      const nextChannels = parseChannelsJSON(next[IMAGE_GEN_CHANNELS_KEY]);
      setChannels(nextChannels);
      setSavedChannels(nextChannels);
      toast.success(t("toast.updated"));
    } catch (error) {
      toast.error(t("toast.saveFailed"), { description: resolveAdminErrorMessage(error) });
    } finally {
      setSaving(false);
    }
  }, [channels, dirty, t, values]);

  function updateChannel(index: number, patch: Partial<ImageGenChannel>) {
    setChannels((prev) => prev.map((item, i) => (i === index ? { ...item, ...patch } : item)));
  }

  function removeChannel(index: number) {
    setChannels((prev) => prev.filter((_, i) => i !== index));
  }

  function moveChannel(index: number, direction: -1 | 1) {
    setChannels((prev) => {
      const target = index + direction;
      if (target < 0 || target >= prev.length) {
        return prev;
      }
      const next = [...prev];
      const [item] = next.splice(index, 1);
      next.splice(target, 0, item);
      return next;
    });
  }

  function addChannel() {
    setChannels((prev) => [...prev, { model: "", note: "" }]);
  }

  const imageGenEnabled = values[IMAGE_GEN_ENABLED_KEY] === "true";

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

      <SettingsSectionSeparator />

      <SettingsSection title={t("imageGen.title")}>
        <p className="mb-3 text-xs leading-5 text-muted-foreground">{t("imageGen.description")}</p>
        <SettingsFieldList>
          <SettingsFieldItem>
            <SettingsFieldRow
              title={t("imageGen.enabledLabel")}
              description={t("imageGen.enabledDescription")}
            >
              <Switch
                checked={imageGenEnabled}
                onCheckedChange={(checked) => {
                  setValues((prev) => ({ ...prev, [IMAGE_GEN_ENABLED_KEY]: checked ? "true" : "false" }));
                }}
                disabled={saving}
              />
            </SettingsFieldRow>
          </SettingsFieldItem>
          <SettingsFieldItem index={1}>
            <SettingsFieldRow
              title={t("imageGen.channelsLabel")}
              description={t("imageGen.channelsDescription")}
            />
          </SettingsFieldItem>
        </SettingsFieldList>

        <div className="mt-1 space-y-2">
          {channels.length === 0 ? (
            <p className="text-xs leading-5 text-muted-foreground">
              {modelsLoading ? "…" : imageGenModels.length === 0 ? t("imageGen.noImageGenModels") : ""}
            </p>
          ) : null}
          {channels.map((channel, index) => (
            <div key={index} className="flex items-center gap-2">
              <div className="flex flex-col">
                <button
                  type="button"
                  aria-label="up"
                  className="flex size-4 items-center justify-center rounded text-muted-foreground hover:bg-muted hover:text-foreground disabled:opacity-30"
                  disabled={saving || index === 0}
                  onClick={() => moveChannel(index, -1)}
                >
                  <ArrowUp className="size-3" />
                </button>
                <button
                  type="button"
                  aria-label="down"
                  className="flex size-4 items-center justify-center rounded text-muted-foreground hover:bg-muted hover:text-foreground disabled:opacity-30"
                  disabled={saving || index === channels.length - 1}
                  onClick={() => moveChannel(index, 1)}
                >
                  <ArrowDown className="size-3" />
                </button>
              </div>
              <Select
                value={channel.model}
                onValueChange={(value) => updateChannel(index, { model: value })}
                disabled={saving}
              >
                <SelectTrigger className="h-8 w-64 min-w-0 text-xs">
                  <SelectValue placeholder={t("imageGen.modelPlaceholder")} />
                </SelectTrigger>
                <SelectContent>
                  {imageGenModels.map((model) => (
                    <SelectItem key={model.platformModelName} value={model.platformModelName} className="text-xs">
                      {model.platformModelName}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <Input
                value={channel.note}
                placeholder={t("imageGen.notePlaceholder")}
                className="h-8 min-w-0 flex-1 text-xs"
                onChange={(event) => updateChannel(index, { note: event.target.value })}
                disabled={saving}
              />
              <Button
                type="button"
                variant="ghost"
                size="icon"
                className="size-8 shrink-0 text-muted-foreground hover:text-destructive"
                disabled={saving}
                onClick={() => removeChannel(index)}
              >
                <Trash2 className="size-3.5" />
              </Button>
            </div>
          ))}
          <Button
            type="button"
            variant="secondary"
            size="sm"
            className="h-8 text-xs font-normal shadow-none"
            disabled={saving || imageGenModels.length === 0}
            onClick={addChannel}
          >
            <Plus className="size-3.5" />
            {t("imageGen.addChannel")}
          </Button>
        </div>
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
