"use client";

import * as React from "react";
import {
  closestCenter,
  DndContext,
  KeyboardSensor,
  PointerSensor,
  type DragEndEvent,
  useSensor,
  useSensors,
} from "@dnd-kit/core";
import {
  SortableContext,
  sortableKeyboardCoordinates,
  useSortable,
  verticalListSortingStrategy,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { Check } from "lucide-react";

import { GripVerticalIcon } from "@/components/ui/grip-vertical";
import type { ChatModelOption } from "@/features/chat/types/chat-runtime";
import { ModelIcon } from "@/shared/components/model-icon";
import { resolveModelIconURL, resolveModelIdentity } from "@/shared/lib/model-identity";
import { cn } from "@/lib/utils";

/**
 * ChatModelSortPanel 模型"自定义排序"视图：flat 列表拖拽排序，
 * 顺序变化时通过 onOrderChange 通知父组件持久化（user settings chat.model_order）。
 * 行样式与分组视图的 ChatModelMenuItem 保持一致（h-7 / text-[11px]）。
 */
export function ChatModelSortPanel({
  modelOptions,
  selectedPlatformModelName,
  onSelect,
  onOrderChange,
}: {
  modelOptions: ChatModelOption[];
  selectedPlatformModelName: string;
  onSelect: (platformModelName: string) => void;
  onOrderChange: (order: string[]) => void;
}) {
  const [items, setItems] = React.useState<ChatModelOption[]>(modelOptions);

  React.useEffect(() => {
    setItems(modelOptions);
  }, [modelOptions]);

  const handleDragEnd = React.useCallback(
    (event: DragEndEvent) => {
      const { active, over } = event;
      if (!over || active.id === over.id) {
        return;
      }
      setItems((current) => {
        const fromIndex = current.findIndex((item) => item.platformModelName === active.id);
        const toIndex = current.findIndex((item) => item.platformModelName === over.id);
        if (fromIndex < 0 || toIndex < 0 || fromIndex === toIndex) {
          return current;
        }
        const next = [...current];
        const [moved] = next.splice(fromIndex, 1);
        next.splice(toIndex, 0, moved);
        onOrderChange(next.map((item) => item.platformModelName));
        return next;
      });
    },
    [onOrderChange],
  );

  const sensors = useSensors(
    useSensor(PointerSensor, {
      activationConstraint: {
        distance: 4,
      },
    }),
    useSensor(KeyboardSensor, {
      coordinateGetter: sortableKeyboardCoordinates,
    }),
  );

  return (
    <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={handleDragEnd}>
      <SortableContext items={items.map((item) => item.platformModelName)} strategy={verticalListSortingStrategy}>
        <div className="flex flex-col gap-0.5">
          {items.map((item) => (
            <ModelSortableRow
              key={item.platformModelName}
              model={item}
              selected={item.platformModelName === selectedPlatformModelName}
              onSelect={onSelect}
            />
          ))}
        </div>
      </SortableContext>
    </DndContext>
  );
}

function ModelSortableRow({
  model,
  selected,
  onSelect,
}: {
  model: ChatModelOption;
  selected: boolean;
  onSelect: (platformModelName: string) => void;
}) {
  const { attributes, isDragging, listeners, setNodeRef, transform, transition } = useSortable({
    id: model.platformModelName,
  });
  const style = {
    transform: CSS.Transform.toString(transform),
    transition,
  } satisfies React.CSSProperties;
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

  return (
    <div
      ref={setNodeRef}
      style={style}
      className={cn(
        "group flex h-7 items-center rounded-md text-[11px] font-medium text-muted-foreground transition-colors",
        isDragging
          ? "bg-accent text-accent-foreground shadow-sm"
          : "hover:bg-accent hover:text-accent-foreground",
      )}
    >
      <button
        {...attributes}
        {...listeners}
        type="button"
        aria-label={platformModelName}
        title={platformModelName}
        className="ml-1 flex size-4 shrink-0 cursor-grab items-center justify-center rounded text-muted-foreground/50 transition-colors hover:text-muted-foreground active:cursor-grabbing"
      >
        <GripVerticalIcon size={11} className="size-3" />
      </button>
      <button
        type="button"
        className="flex h-7 min-w-0 flex-1 items-center gap-2 rounded-md bg-transparent py-0 pl-1 pr-2 text-left text-[11px] font-medium leading-none text-inherit outline-none"
        onClick={() => onSelect(model.platformModelName)}
      >
        <ModelIcon iconUrl={iconURL} label={platformModelName} />
        <span className="min-w-0 flex-1 truncate leading-4">{platformModelName}</span>
        {selected ? <Check className="size-3 shrink-0 text-current" strokeWidth={1.7} /> : null}
      </button>
    </div>
  );
}
