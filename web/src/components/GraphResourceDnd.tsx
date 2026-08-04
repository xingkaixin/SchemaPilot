import {
  DndContext,
  DragOverlay,
  KeyboardSensor,
  PointerSensor,
  pointerWithin,
  rectIntersection,
  useSensor,
  useSensors,
  type CollisionDetection,
  type DragEndEvent,
  type DragStartEvent,
} from "@dnd-kit/core";
import { FileCode2, Folder } from "lucide-react";
import { useState, type ReactNode } from "react";
import { graphDropTargetId, isGraphResource, type GraphResource } from "../lib/graphResources";
import { useMigratorStore } from "../store";

const graphCollisionDetection: CollisionDetection = (arguments_) => {
  const pointerCollisions = pointerWithin(arguments_);
  return pointerCollisions.length > 0 ? pointerCollisions : rectIntersection(arguments_);
};

export function GraphResourceDnd({
  database,
  children,
}: {
  database: string;
  children: ReactNode;
}) {
  const addResource = useMigratorStore((state) => state.addResource);
  const [activeResource, setActiveResource] = useState<GraphResource | null>(null);
  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 6 } }),
    useSensor(KeyboardSensor),
  );

  const handleDragStart = (event: DragStartEvent) => {
    const resource = event.active.data.current?.resource;
    setActiveResource(isGraphResource(resource) ? resource : null);
  };
  const handleDragEnd = (event: DragEndEvent) => {
    const resource = event.active.data.current?.resource;
    if (event.over?.id === graphDropTargetId && isGraphResource(resource)) {
      addResource(resource, database);
    }
    setActiveResource(null);
  };

  return (
    <DndContext
      sensors={sensors}
      collisionDetection={graphCollisionDetection}
      onDragStart={handleDragStart}
      onDragCancel={() => setActiveResource(null)}
      onDragEnd={handleDragEnd}
    >
      {children}
      <DragOverlay dropAnimation={null}>
        {activeResource ? <GraphResourcePreview resource={activeResource} /> : null}
      </DragOverlay>
    </DndContext>
  );
}

function GraphResourcePreview({ resource }: { resource: GraphResource }) {
  const Icon = resource.kind === "directory" ? Folder : FileCode2;
  return (
    <div className="drag-resource-overlay">
      <Icon size={14} />
      <code>{resource.path}</code>
      {resource.kind === "directory" ? <span>{resource.scripts.length} SQL</span> : null}
    </div>
  );
}
