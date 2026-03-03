"use client";

import React, { useState, useCallback, useRef, useEffect } from "react";
import { usePravaraSession } from "@/lib/auth";
import { useQueryClient } from "@tanstack/react-query";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Save, RotateCcw, Move, Grid } from "lucide-react";
import { useToast } from "@/components/ui/use-toast";
import { useMachineStore, type Machine } from "@/stores/machineStore";
import { useFactoryLayout } from "@/hooks/useFactoryLayout";
import {
  layoutsAPI,
  modelsAPI,
  type LayoutMachinePosition,
  type MachineModel,
} from "@/lib/api";

interface DragState {
  machineId: string;
  offsetX: number;
  offsetY: number;
}

const GRID_SIZE = 20; // pixels per meter
const FLOOR_WIDTH = 30; // meters
const FLOOR_HEIGHT = 30; // meters

export const LayoutEditor: React.FC = () => {
  const { data: session } = usePravaraSession();
  const token = session?.accessToken as string | undefined;
  const { toast } = useToast();
  const queryClient = useQueryClient();
  const canvasRef = useRef<HTMLCanvasElement>(null);

  const machines = useMachineStore((state) => state.machines);
  const { layout, isLoading } = useFactoryLayout();

  const [positions, setPositions] = useState<Map<string, LayoutMachinePosition>>(
    new Map()
  );
  const [selectedMachine, setSelectedMachine] = useState<string | null>(null);
  const [dragState, setDragState] = useState<DragState | null>(null);
  const [models, setModels] = useState<MachineModel[]>([]);
  const [modelAssignments, setModelAssignments] = useState<
    Map<string, string>
  >(new Map());
  const [snapToGrid, setSnapToGrid] = useState(true);
  const [isSaving, setIsSaving] = useState(false);

  // Load models list
  useEffect(() => {
    if (!token) return;
    modelsAPI.list(token).then(setModels).catch(() => {});
  }, [token]);

  // Initialize positions from layout
  useEffect(() => {
    if (!layout?.machine_positions) return;
    const map = new Map<string, LayoutMachinePosition>();
    for (const mp of layout.machine_positions) {
      map.set(mp.machine_id, mp);
    }
    setPositions(map);
  }, [layout]);

  // Draw the 2D overhead view
  const draw = useCallback(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    const ctx = canvas.getContext("2d");
    if (!ctx) return;

    const width = canvas.width;
    const height = canvas.height;

    // Clear
    ctx.fillStyle = "#1a1a2e";
    ctx.fillRect(0, 0, width, height);

    // Grid
    ctx.strokeStyle = "#ffffff10";
    ctx.lineWidth = 1;
    for (let x = 0; x <= width; x += GRID_SIZE) {
      ctx.beginPath();
      ctx.moveTo(x, 0);
      ctx.lineTo(x, height);
      ctx.stroke();
    }
    for (let y = 0; y <= height; y += GRID_SIZE) {
      ctx.beginPath();
      ctx.moveTo(0, y);
      ctx.lineTo(width, y);
      ctx.stroke();
    }

    // Major grid lines every 5m
    ctx.strokeStyle = "#ffffff20";
    ctx.lineWidth = 2;
    for (let x = 0; x <= width; x += GRID_SIZE * 5) {
      ctx.beginPath();
      ctx.moveTo(x, 0);
      ctx.lineTo(x, height);
      ctx.stroke();
    }
    for (let y = 0; y <= height; y += GRID_SIZE * 5) {
      ctx.beginPath();
      ctx.moveTo(0, y);
      ctx.lineTo(width, y);
      ctx.stroke();
    }

    // Draw machines
    const machineList = Object.values(machines);
    machineList.forEach((machine, index) => {
      const pos = positions.get(machine.id);
      // Convert from meters to canvas pixels, centered on canvas
      const px = pos
        ? (pos.position.x + FLOOR_WIDTH / 2) * GRID_SIZE
        : ((index % 5) * 3 + 2) * GRID_SIZE;
      const py = pos
        ? (pos.position.z + FLOOR_HEIGHT / 2) * GRID_SIZE
        : (Math.floor(index / 5) * 3 + 2) * GRID_SIZE;

      const size = GRID_SIZE * 1.5;

      // Machine box
      const isSelected = selectedMachine === machine.id;
      const statusColors: Record<string, string> = {
        online: "#22c55e",
        offline: "#6b7280",
        error: "#ef4444",
        maintenance: "#f59e0b",
      };

      ctx.fillStyle = isSelected ? "#3b82f6" : (statusColors[machine.status] ?? "#6b7280");
      ctx.fillRect(px - size / 2, py - size / 2, size, size);

      // Selection outline
      if (isSelected) {
        ctx.strokeStyle = "#ffffff";
        ctx.lineWidth = 2;
        ctx.strokeRect(px - size / 2 - 2, py - size / 2 - 2, size + 4, size + 4);
      }

      // Label
      ctx.fillStyle = "#ffffff";
      ctx.font = "10px sans-serif";
      ctx.textAlign = "center";
      ctx.fillText(
        machine.name || machine.id.slice(0, 8),
        px,
        py + size / 2 + 12
      );
    });
  }, [machines, positions, selectedMachine]);

  useEffect(() => {
    draw();
  }, [draw]);

  // Mouse handlers for drag-and-drop
  const handleMouseDown = (e: React.MouseEvent<HTMLCanvasElement>) => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    const rect = canvas.getBoundingClientRect();
    const mx = e.clientX - rect.left;
    const my = e.clientY - rect.top;

    // Find clicked machine
    const machineList = Object.values(machines);
    for (let i = machineList.length - 1; i >= 0; i--) {
      const machine = machineList[i];
      const pos = positions.get(machine.id);
      const px = pos
        ? (pos.position.x + FLOOR_WIDTH / 2) * GRID_SIZE
        : ((i % 5) * 3 + 2) * GRID_SIZE;
      const py = pos
        ? (pos.position.z + FLOOR_HEIGHT / 2) * GRID_SIZE
        : (Math.floor(i / 5) * 3 + 2) * GRID_SIZE;

      const size = GRID_SIZE * 1.5;
      if (
        mx >= px - size / 2 &&
        mx <= px + size / 2 &&
        my >= py - size / 2 &&
        my <= py + size / 2
      ) {
        setSelectedMachine(machine.id);
        setDragState({
          machineId: machine.id,
          offsetX: mx - px,
          offsetY: my - py,
        });
        return;
      }
    }

    setSelectedMachine(null);
  };

  const handleMouseMove = (e: React.MouseEvent<HTMLCanvasElement>) => {
    if (!dragState) return;
    const canvas = canvasRef.current;
    if (!canvas) return;
    const rect = canvas.getBoundingClientRect();
    let mx = e.clientX - rect.left - dragState.offsetX;
    let my = e.clientY - rect.top - dragState.offsetY;

    // Convert to meters
    let posX = mx / GRID_SIZE - FLOOR_WIDTH / 2;
    let posZ = my / GRID_SIZE - FLOOR_HEIGHT / 2;

    if (snapToGrid) {
      posX = Math.round(posX);
      posZ = Math.round(posZ);
    }

    setPositions((prev) => {
      const next = new Map(prev);
      const existing = next.get(dragState.machineId);
      next.set(dragState.machineId, {
        machine_id: dragState.machineId,
        position: { x: posX, y: 0, z: posZ },
        rotation: existing?.rotation ?? { x: 0, y: 0, z: 0 },
        scale: existing?.scale ?? 1,
        visible: true,
      });
      return next;
    });
  };

  const handleMouseUp = () => {
    setDragState(null);
  };

  // Save layout
  const handleSave = async () => {
    if (!token || !layout) return;
    setIsSaving(true);

    try {
      await layoutsAPI.update(token, layout.id, {
        machine_positions: Array.from(positions.values()),
      });
      queryClient.invalidateQueries({ queryKey: ["layouts"] });
      toast({ title: "Layout saved", description: "Machine positions updated" });
    } catch (err) {
      toast({
        title: "Save failed",
        description: err instanceof Error ? err.message : "Unknown error",
        variant: "destructive",
      });
    } finally {
      setIsSaving(false);
    }
  };

  // Reset positions
  const handleReset = () => {
    if (layout?.machine_positions) {
      const map = new Map<string, LayoutMachinePosition>();
      for (const mp of layout.machine_positions) {
        map.set(mp.machine_id, mp);
      }
      setPositions(map);
    } else {
      setPositions(new Map());
    }
  };

  if (isLoading) {
    return (
      <Card>
        <CardContent className="p-8 text-center text-muted-foreground">
          Loading layout...
        </CardContent>
      </Card>
    );
  }

  return (
    <div className="flex gap-4">
      {/* Canvas area */}
      <Card className="flex-1">
        <CardHeader className="pb-2">
          <div className="flex items-center justify-between">
            <CardTitle className="flex items-center gap-2">
              <Grid className="h-5 w-5" />
              Factory Floor Layout
            </CardTitle>
            <div className="flex gap-2">
              <Button
                variant="outline"
                size="sm"
                onClick={() => setSnapToGrid(!snapToGrid)}
              >
                <Move className="mr-1 h-4 w-4" />
                Snap: {snapToGrid ? "On" : "Off"}
              </Button>
              <Button variant="outline" size="sm" onClick={handleReset}>
                <RotateCcw className="mr-1 h-4 w-4" />
                Reset
              </Button>
              <Button size="sm" onClick={handleSave} disabled={isSaving}>
                <Save className="mr-1 h-4 w-4" />
                {isSaving ? "Saving..." : "Save Layout"}
              </Button>
            </div>
          </div>
        </CardHeader>
        <CardContent>
          <canvas
            ref={canvasRef}
            width={FLOOR_WIDTH * GRID_SIZE}
            height={FLOOR_HEIGHT * GRID_SIZE}
            className="border rounded cursor-crosshair"
            onMouseDown={handleMouseDown}
            onMouseMove={handleMouseMove}
            onMouseUp={handleMouseUp}
            onMouseLeave={handleMouseUp}
          />
        </CardContent>
      </Card>

      {/* Properties panel */}
      <Card className="w-72">
        <CardHeader>
          <CardTitle className="text-sm">Machine Properties</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          {selectedMachine ? (
            <>
              <div>
                <p className="text-sm font-medium">
                  {machines[selectedMachine]?.name ?? "Unknown"}
                </p>
                <p className="text-xs text-muted-foreground">
                  {selectedMachine.slice(0, 8)}
                </p>
              </div>

              <div className="text-xs space-y-1">
                <p>
                  Position:{" "}
                  {positions.get(selectedMachine)
                    ? `(${positions.get(selectedMachine)!.position.x.toFixed(1)}, ${positions.get(selectedMachine)!.position.z.toFixed(1)})`
                    : "Not placed"}
                </p>
                <p>Status: {machines[selectedMachine]?.status}</p>
                <p>Type: {machines[selectedMachine]?.type}</p>
              </div>

              {/* Model assignment */}
              <div className="space-y-2">
                <p className="text-xs font-medium">3D Model</p>
                <Select
                  value={modelAssignments.get(selectedMachine) ?? ""}
                  onValueChange={(val) => {
                    setModelAssignments((prev) => {
                      const next = new Map(prev);
                      next.set(selectedMachine!, val);
                      return next;
                    });
                  }}
                >
                  <SelectTrigger className="h-8 text-xs">
                    <SelectValue placeholder="Assign model..." />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="none">No model</SelectItem>
                    {models.map((m) => (
                      <SelectItem key={m.id} value={m.id}>
                        {m.name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            </>
          ) : (
            <p className="text-sm text-muted-foreground">
              Click a machine on the floor to select it, then drag to reposition.
            </p>
          )}
        </CardContent>
      </Card>
    </div>
  );
};
