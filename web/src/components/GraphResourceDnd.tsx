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
  type DragStartEvent,
} from "@dnd-kit/core";
import { FileCode2, Folder } from "lucide-react";
import { useState, type ReactNode } from "react";
import { isGraphResource, type GraphResource } from "../lib/graphResources";

const graphCollisionDetection: CollisionDetection = (arguments_) => {
  const pointerCollisions = pointerWithin(arguments_);
  return pointerCollisions.length > 0 ? pointerCollisions : rectIntersection(arguments_);
};

// Dropping onto the canvas is handled by FlowSurface via useDndMonitor, which
// can translate the release point into flow coordinates.
export function GraphResourceDnd({ children }: { children: ReactNode }) {
  const [activeResource, setActiveResource] = useState<GraphResource | null>(null);
  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 6 } }),
    useSensor(KeyboardSensor),
  );

  const handleDragStart = (event: DragStartEvent) => {
    const resource = event.active.data.current?.resource;
    setActiveResource(isGraphResource(resource) ? resource : null);
  };

  return (
    <DndContext
      sensors={sensors}
      collisionDetection={graphCollisionDetection}
      onDragStart={handleDragStart}
      onDragCancel={() => setActiveResource(null)}
      onDragEnd={() => setActiveResource(null)}
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
