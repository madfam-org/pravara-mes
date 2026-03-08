"use client";

import React, { Suspense, useRef, useEffect, useState } from "react";
import { Canvas, useFrame, useThree } from "@react-three/fiber";
import {
  OrbitControls,
  Grid,
  GizmoHelper,
  GizmoViewport,
  PerspectiveCamera,
  Environment,
  Text,
  Box,
  Sphere,
  Line,
  Html,
  useGLTF,
  Loader,
  Stats,
} from "@react-three/drei";
import { useControls, folder } from "leva";
import * as THREE from "three";
import { useRealtimeConnection } from "@/hooks/useRealtimeConnection";
import { useTelemetryUpdates } from "@/hooks/useTelemetryUpdates";
import { useFactoryLayout } from "@/hooks/useFactoryLayout";
import { useMachineStore } from "@/stores/machineStore";

// Types
interface MachinePosition {
  id: string;
  position: [number, number, number];
  rotation: [number, number, number];
  scale: [number, number, number];
  status: "idle" | "running" | "error" | "maintenance";
  toolPosition?: [number, number, number];
  modelUrl?: string;
}

interface FactoryLayout {
  id: string;
  name: string;
  machines: MachinePosition[];
  gridSize: [number, number];
  cameraPresets: CameraPreset[];
}

interface CameraPreset {
  name: string;
  position: [number, number, number];
  target: [number, number, number];
}

// Machine component with GLTF model or fallback box
const Machine: React.FC<{
  machine: MachinePosition;
  selected: boolean;
  onSelect: () => void;
}> = ({ machine, selected, onSelect }) => {
  const meshRef = useRef<THREE.Mesh>(null);
  const [hovered, setHovered] = useState(false);

  // Load GLTF model if available
  const model = machine.modelUrl ? useGLTF(machine.modelUrl, true) : null;

  // Animate machine when running
  useFrame((state, delta) => {
    if (meshRef.current && machine.status === "running") {
      // Subtle vibration when running
      meshRef.current.position.y = machine.position[1] + Math.sin(state.clock.elapsedTime * 10) * 0.002;
    }
  });

  // Status colors
  const statusColors = {
    idle: "#888888",
    running: "#00ff00",
    error: "#ff0000",
    maintenance: "#ffaa00",
  };

  return (
    <group position={machine.position} rotation={machine.rotation} scale={machine.scale}>
      {model ? (
        <primitive
          ref={meshRef}
          object={model.scene.clone()}
          onClick={onSelect}
          onPointerOver={() => setHovered(true)}
          onPointerOut={() => setHovered(false)}
        />
      ) : (
        <Box
          ref={meshRef}
          args={[1, 1.5, 1]}
          onClick={onSelect}
          onPointerOver={() => setHovered(true)}
          onPointerOut={() => setHovered(false)}
        >
          <meshStandardMaterial
            color={selected ? "#0066ff" : statusColors[machine.status]}
            emissive={hovered ? "#ffffff" : "#000000"}
            emissiveIntensity={hovered ? 0.2 : 0}
          />
        </Box>
      )}

      {/* Status indicator sphere */}
      <Sphere args={[0.1]} position={[0, 2, 0]}>
        <meshStandardMaterial
          color={statusColors[machine.status]}
          emissive={statusColors[machine.status]}
          emissiveIntensity={0.5}
        />
      </Sphere>

      {/* Machine label */}
      <Html position={[0, 2.5, 0]} center>
        <div className="bg-black/75 text-white px-2 py-1 rounded text-xs whitespace-nowrap">
          Machine {machine.id.slice(0, 8)}
          <div className="text-[10px] opacity-75">{machine.status}</div>
        </div>
      </Html>

      {/* Tool position indicator if machine is running */}
      {machine.toolPosition && machine.status === "running" && (
        <Sphere args={[0.05]} position={machine.toolPosition}>
          <meshStandardMaterial color="#00ffff" emissive="#00ffff" emissiveIntensity={1} />
        </Sphere>
      )}
    </group>
  );
};

// Tool path visualization
const ToolPath: React.FC<{ path: Array<[number, number, number]> }> = ({ path }) => {
  if (path.length < 2) return null;

  return (
    <Line
      points={path}
      color="#00ff00"
      lineWidth={2}
      dashed={false}
    />
  );
};

// Factory floor grid and boundaries
const FactoryFloorGrid: React.FC<{ size: [number, number] }> = ({ size }) => {
  const { gridVisible, gridOpacity } = useControls("Grid", {
    gridVisible: { value: true, label: "Show Grid" },
    gridOpacity: { value: 0.5, min: 0, max: 1, label: "Grid Opacity" },
  });

  return gridVisible ? (
    <Grid
      args={size}
      cellSize={1}
      cellThickness={0.5}
      cellColor="#666666"
      sectionSize={5}
      sectionThickness={1}
      sectionColor="#999999"
      fadeDistance={50}
      fadeStrength={1}
      followCamera={false}
      infiniteGrid={false}
    />
  ) : null;
};

// Camera controller with presets
const CameraController: React.FC<{ presets: CameraPreset[] }> = ({ presets }) => {
  const { camera } = useThree();
  const controlsRef = useRef<any>(null);

  const { activePreset } = useControls("Camera", {
    activePreset: {
      value: "default",
      options: ["default", ...presets.map(p => p.name)],
      label: "Camera Preset",
    },
  });

  useEffect(() => {
    const preset = presets.find(p => p.name === activePreset);
    if (preset && controlsRef.current) {
      camera.position.set(...preset.position);
      controlsRef.current.target.set(...preset.target);
      controlsRef.current.update();
    }
  }, [activePreset, presets, camera]);

  return (
    <OrbitControls
      ref={controlsRef}
      enablePan={true}
      enableZoom={true}
      enableRotate={true}
      maxPolarAngle={Math.PI * 0.85}
      minDistance={5}
      maxDistance={100}
    />
  );
};

// Main factory floor component
export const FactoryFloor3D: React.FC = () => {
  const [selectedMachine, setSelectedMachine] = useState<string | null>(null);
  const [toolPath, setToolPath] = useState<Array<[number, number, number]>>([]);

  const machines = useMachineStore((state) => state.machines);
  const { isConnected } = useRealtimeConnection();
  const { getLatestMetric } = useTelemetryUpdates();
  const { layout } = useFactoryLayout();

  // Build tool position from individual telemetry metrics
  const getToolPosition = React.useCallback(
    (machineId: string): [number, number, number] | undefined => {
      const px = getLatestMetric(machineId, "position_x");
      const py = getLatestMetric(machineId, "position_y");
      const pz = getLatestMetric(machineId, "position_z");
      if (px && py && pz) {
        return [px.value, pz.value, py.value]; // Map Y→Z for 3D scene (Y-up)
      }
      return undefined;
    },
    [getLatestMetric]
  );

  // Build lookup from layout positions
  const layoutPositionMap = React.useMemo(() => {
    const map = new Map<string, { position: [number, number, number]; rotation: [number, number, number] }>();
    if (layout?.machine_positions) {
      for (const mp of layout.machine_positions) {
        map.set(mp.machine_id, {
          position: [mp.position.x, mp.position.y, mp.position.z],
          rotation: [mp.rotation.x, mp.rotation.y, mp.rotation.z],
        });
      }
    }
    return map;
  }, [layout]);

  // Convert machines to 3D positions
  const machinePositions: MachinePosition[] = React.useMemo(() => {
    return Object.values(machines).map((machine, index) => {
      // Priority: layout DB position > machine store position > grid fallback
      const layoutPos = layoutPositionMap.get(machine.id);
      const gridPosition: [number, number, number] = [
        (index % 5) * 3 - 6,
        0,
        Math.floor(index / 5) * 3 - 3,
      ];

      return {
        id: machine.id,
        position: layoutPos?.position ?? machine.position ?? gridPosition,
        rotation: layoutPos?.rotation ?? machine.rotation ?? [0, 0, 0] as [number, number, number],
        scale: [1, 1, 1] as [number, number, number],
        status: (machine.status === "online"
          ? "running"
          : machine.status === "offline"
          ? "idle"
          : machine.status === "maintenance"
          ? "maintenance"
          : "error") as MachinePosition["status"],
        toolPosition: getToolPosition(machine.id),
        modelUrl: machine.modelUrl,
      };
    });
  }, [machines, getToolPosition, layoutPositionMap]);

  // Leva controls
  const {
    ambientIntensity,
    directionalIntensity,
    shadowsEnabled,
    environmentPreset,
    showStats,
  } = useControls("Scene", {
    ambientIntensity: { value: 0.5, min: 0, max: 1, label: "Ambient Light" },
    directionalIntensity: { value: 1, min: 0, max: 2, label: "Directional Light" },
    shadowsEnabled: { value: true, label: "Enable Shadows" },
    environmentPreset: {
      value: "warehouse",
      options: ["warehouse", "sunset", "dawn", "night", "forest", "apartment", "studio", "city", "park", "lobby"],
      label: "Environment",
    },
    showStats: { value: false, label: "Show Stats" },
  });

  const defaultLayout: FactoryLayout = {
    id: "default",
    name: "Main Factory Floor",
    machines: machinePositions,
    gridSize: [30, 30],
    cameraPresets: [
      { name: "overview", position: [20, 15, 20], target: [0, 0, 0] },
      { name: "front", position: [0, 5, 15], target: [0, 0, 0] },
      { name: "side", position: [15, 5, 0], target: [0, 0, 0] },
      { name: "top", position: [0, 25, 0], target: [0, 0, 0] },
    ],
  };

  return (
    <div className="w-full h-full relative">
      {/* Connection status */}
      <div className="absolute top-4 left-4 z-10 bg-black/75 text-white px-3 py-2 rounded">
        <div className="flex items-center gap-2">
          <div className={`w-2 h-2 rounded-full ${isConnected ? "bg-green-500" : "bg-red-500"}`} />
          <span className="text-sm">{isConnected ? "Connected" : "Disconnected"}</span>
        </div>
      </div>

      {/* Selected machine info */}
      {selectedMachine && (
        <div className="absolute top-4 right-4 z-10 bg-black/75 text-white p-4 rounded min-w-[200px]">
          <h3 className="font-semibold mb-2">Machine Details</h3>
          <div className="text-sm space-y-1">
            <div>ID: {selectedMachine.slice(0, 8)}</div>
            <div>Status: {machines[selectedMachine]?.status}</div>
            <div>Type: {machines[selectedMachine]?.type}</div>
          </div>
        </div>
      )}

      {/* Three.js Canvas */}
      <Canvas
        shadows={shadowsEnabled}
        camera={{ position: [15, 10, 15], fov: 60 }}
        gl={{ preserveDrawingBuffer: true, antialias: true }}
      >
        {/* Lighting */}
        <ambientLight intensity={ambientIntensity} />
        <directionalLight
          position={[10, 10, 5]}
          intensity={directionalIntensity}
          castShadow={shadowsEnabled}
          shadow-mapSize={[2048, 2048]}
        />

        {/* Environment */}
        <Environment preset={environmentPreset as any} background />

        {/* Camera controls */}
        <CameraController presets={defaultLayout.cameraPresets} />

        {/* Gizmo helper */}
        <GizmoHelper alignment="bottom-right" margin={[80, 80]}>
          <GizmoViewport axisColors={["red", "green", "blue"]} labelColor="black" />
        </GizmoHelper>

        {/* Factory floor */}
        <FactoryFloorGrid size={defaultLayout.gridSize} />

        {/* Machines */}
        <Suspense fallback={null}>
          {machinePositions.map((machine) => (
            <Machine
              key={machine.id}
              machine={machine}
              selected={selectedMachine === machine.id}
              onSelect={() => setSelectedMachine(machine.id)}
            />
          ))}
        </Suspense>

        {/* Tool path visualization */}
        {toolPath.length > 0 && <ToolPath path={toolPath} />}

        {/* Performance stats */}
        {showStats && <Stats />}
      </Canvas>

      {/* Loading indicator */}
      <Loader />
    </div>
  );
};