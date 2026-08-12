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

import { GripVerticalIcon } from "@/components/ui/grip-vertical";
import type { ChatModelOption } from "@/features/chat/types/chat-runtime";
import { ChatModelIdentity } from "./chat-model-identity";
import { cn } from "@/lib/utils";

/**
 * ChatModelSortPanel 模型"自定义排序"视图：flat 列表拖拽排序，
 * 顺序变化时通过 onOrderChange 通知父组件持久化（user settings chat.model_order）。
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
    <div className="flex flex-col gap-1">
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
    </div>
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

  return (
    <div
      ref={setNodeRef}
      style={style}
      className={cn(
        "flex h-8 w-full min-w-0 items-center gap-1 rounded-md pl-2 pr-1 text-left transition-colors",
        isDragging ? "bg-accent shadow-sm" : "hover:bg-accent/70",
      )}
    >
      <button
        {...attributes}
        {...listeners}
        type="button"
        aria-label={model.platformModelName}
        title={model.platformModelName}
        className="flex size-5 shrink-0 cursor-grab items-center justify-center rounded-md text-muted-foreground/50 transition-colors hover:bg-accent hover:text-muted-foreground active:cursor-grabbing"
      >
        <GripVerticalIcon size={12} className="size-3.5" />
      </button>
      <button
        type="button"
        className="flex h-full min-w-0 flex-1 items-center rounded-md outline-none"
        onClick={() => onSelect(model.platformModelName)}
      >
        <ChatModelIdentity model={model} density="compact" />
      </button>
    </div>
  );
}
