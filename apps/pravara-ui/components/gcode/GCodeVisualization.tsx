'use client';

import React, { useRef, useMemo, useState, useEffect } from 'react';
import { Canvas, useFrame, useThree } from '@react-three/fiber';
import { OrbitControls, Grid, Line, Box, Text } from '@react-three/drei';
import * as THREE from 'three';

interface ExtrusionSegment {
  start: { x: number; y: number; z: number };
  end: { x: number; y: number; z: number };
  extrusion_rate: number;
  layer_height: number;
  line_width: number;
  temperature: number;
  speed: number;
  material: string;
  is_retraction: boolean;
  is_prime: boolean;
  is_travel: boolean;
  volume_deposited: number;
}

interface Layer {
  number: number;
  height: number;
  segments: ExtrusionSegment[];
  print_time: number;
  filament_used_mm: number;
}

interface GCodeVisualizationProps {
  gcode?: string;
  segments?: ExtrusionSegment[];
  layers?: Layer[];
  currentSegment?: number;
  showTravel?: boolean;
  showRetractions?: boolean;
  layerView?: boolean;
  currentLayer?: number;
  material?: string;
  boundingBox?: {
    min: { x: number; y: number; z: number };
    max: { x: number; y: number; z: number };
  };
  animate?: boolean;
  speedMultiplier?: number;
}

// Component for rendering the toolpath
const ToolPath: React.FC<{
  segments: ExtrusionSegment[];
  currentSegment: number;
  showTravel: boolean;
  showRetractions: boolean;
  material: string;
}> = ({ segments, currentSegment, showTravel, showRetractions, material }) => {
  const lineRef = useRef<THREE.BufferGeometry>(null);

  // Generate line segments and colors
  const { points, colors } = useMemo(() => {
    const pts: number[] = [];
    const cols: number[] = [];

    segments.forEach((segment, index) => {
      // Skip travel moves if not showing
      if (segment.is_travel && !showTravel) return;
      if (segment.is_retraction && !showRetractions) return;

      // Add segment points
      pts.push(segment.start.x, segment.start.y, segment.start.z);
      pts.push(segment.end.x, segment.end.y, segment.end.z);

      // Determine color based on segment type and state
      let color: THREE.Color;

      if (index > currentSegment) {
        // Future segments - semi-transparent
        color = new THREE.Color(0.5, 0.5, 0.5);
      } else if (segment.is_retraction) {
        color = new THREE.Color(1, 0, 0); // Red for retractions
      } else if (segment.is_travel) {
        color = new THREE.Color(0.3, 0.3, 0.3); // Dark gray for travel
      } else if (segment.is_prime) {
        color = new THREE.Color(0, 0, 1); // Blue for prime
      } else {
        // Extrusion color based on speed/temperature
        const speedNorm = Math.min(segment.speed / 100, 1);
        const tempNorm = (segment.temperature - 180) / 70; // Normalize 180-250°C

        if (material === 'PLA') {
          color = new THREE.Color(0.2, 0.8 * speedNorm, 0.2); // Green for PLA
        } else if (material === 'ABS') {
          color = new THREE.Color(0.8 * tempNorm, 0.2, 0.2); // Red for ABS
        } else if (material === 'PETG') {
          color = new THREE.Color(0.2, 0.2, 0.8); // Blue for PETG
        } else {
          color = new THREE.Color(0.5, 0.5, 0.5); // Gray default
        }
      }

      // Add color for both points
      cols.push(color.r, color.g, color.b);
      cols.push(color.r, color.g, color.b);
    });

    return { points: new Float32Array(pts), colors: new Float32Array(cols) };
  }, [segments, currentSegment, showTravel, showRetractions, material]);

  return (
    <lineSegments>
      <bufferGeometry ref={lineRef}>
        <bufferAttribute
          attach="attributes-position"
          count={points.length / 3}
          array={points}
          itemSize={3}
        />
        <bufferAttribute
          attach="attributes-color"
          count={colors.length / 3}
          array={colors}
          itemSize={3}
        />
      </bufferGeometry>
      <lineBasicMaterial vertexColors linewidth={2} />
    </lineSegments>
  );
};

// Component for layer-by-layer view
const LayerView: React.FC<{
  layers: Layer[];
  currentLayer: number;
  showTravel: boolean;
  material: string;
}> = ({ layers, currentLayer, showTravel, material }) => {
  const segments = useMemo(() => {
    const allSegments: ExtrusionSegment[] = [];
    layers.slice(0, currentLayer + 1).forEach((layer) => {
      allSegments.push(...layer.segments);
    });
    return allSegments;
  }, [layers, currentLayer]);

  return (
    <ToolPath
      segments={segments}
      currentSegment={segments.length}
      showTravel={showTravel}
      showRetractions={false}
      material={material}
    />
  );
};

// Material deposition visualization (shows actual material volume)
const MaterialDeposition: React.FC<{
  segments: ExtrusionSegment[];
  currentSegment: number;
  material: string;
}> = ({ segments, currentSegment, material }) => {
  const meshRef = useRef<THREE.Group>(null);

  const depositions = useMemo(() => {
    const deps: JSX.Element[] = [];

    segments.slice(0, currentSegment + 1).forEach((segment, index) => {
      if (segment.is_travel || segment.is_retraction) return;

      const start = new THREE.Vector3(segment.start.x, segment.start.y, segment.start.z);
      const end = new THREE.Vector3(segment.end.x, segment.end.y, segment.end.z);
      const length = start.distanceTo(end);

      if (length === 0) return;

      // Calculate position and rotation for the deposited material
      const position = start.clone().add(end).multiplyScalar(0.5);
      const direction = end.clone().sub(start).normalize();

      // Create a box representing the deposited material
      const width = segment.line_width;
      const height = segment.layer_height;

      deps.push(
        <Box
          key={`dep-${index}`}
          position={[position.x, position.y, position.z]}
          args={[length, width, height]}
          rotation={[
            0,
            Math.atan2(direction.x, direction.z),
            Math.atan2(direction.y, Math.sqrt(direction.x ** 2 + direction.z ** 2))
          ]}
        >
          <meshPhongMaterial
            color={material === 'PLA' ? '#4CAF50' : material === 'ABS' ? '#F44336' : '#2196F3'}
            opacity={0.9}
            transparent
          />
        </Box>
      );
    });

    return deps;
  }, [segments, currentSegment, material]);

  return <group ref={meshRef}>{depositions}</group>;
};

// Animated print head
const PrintHead: React.FC<{
  position: { x: number; y: number; z: number };
  isExtruding: boolean;
}> = ({ position, isExtruding }) => {
  const meshRef = useRef<THREE.Mesh>(null);

  useFrame((state) => {
    if (meshRef.current && isExtruding) {
      // Add slight vibration when extruding
      meshRef.current.position.y = position.y + Math.sin(state.clock.elapsedTime * 20) * 0.05;
    }
  });

  return (
    <group position={[position.x, position.y, position.z]}>
      {/* Nozzle */}
      <mesh ref={meshRef}>
        <coneGeometry args={[2, 5, 8]} />
        <meshStandardMaterial color={isExtruding ? '#ff6b6b' : '#666666'} />
      </mesh>
      {/* Hotend block */}
      <Box position={[0, 3, 0]} args={[6, 4, 6]}>
        <meshStandardMaterial color="#333333" />
      </Box>
    </group>
  );
};

// Build platform
const BuildPlatform: React.FC<{
  size: { x: number; y: number };
  heated?: boolean;
  temperature?: number;
}> = ({ size, heated = false, temperature = 60 }) => {
  const color = heated ? `hsl(${30 - temperature / 10}, 100%, 50%)` : '#666666';

  return (
    <group>
      <Box position={[0, -1, 0]} args={[size.x, 2, size.y]}>
        <meshStandardMaterial color={color} />
      </Box>
      <Grid
        position={[0, 0.01, 0]}
        args={[size.x, size.y]}
        cellSize={10}
        cellThickness={0.5}
        cellColor="#444444"
        sectionSize={50}
        sectionThickness={1}
        sectionColor="#888888"
        fadeDistance={200}
      />
    </group>
  );
};

// Main visualization component
export const GCodeVisualization: React.FC<GCodeVisualizationProps> = ({
  segments = [],
  layers = [],
  currentSegment = 0,
  showTravel = true,
  showRetractions = true,
  layerView = false,
  currentLayer = 0,
  material = 'PLA',
  boundingBox,
  animate = false,
  speedMultiplier = 1,
}) => {
  const [animatedSegment, setAnimatedSegment] = useState(0);
  const [printHeadPos, setPrintHeadPos] = useState({ x: 0, y: 0, z: 0 });
  const [isExtruding, setIsExtruding] = useState(false);

  // Animation loop
  useEffect(() => {
    if (!animate || segments.length === 0) return;

    const interval = setInterval(() => {
      setAnimatedSegment((prev) => {
        const next = prev + 1;
        if (next >= segments.length) {
          return 0; // Loop
        }

        // Update print head position
        const segment = segments[next];
        setPrintHeadPos(segment.end);
        setIsExtruding(!segment.is_travel && !segment.is_retraction);

        return next;
      });
    }, 100 / speedMultiplier); // Adjust speed

    return () => clearInterval(interval);
  }, [animate, segments, speedMultiplier]);

  // Calculate build volume from bounding box
  const buildSize = boundingBox
    ? {
        x: Math.max(200, boundingBox.max.x - boundingBox.min.x + 20),
        y: Math.max(200, boundingBox.max.y - boundingBox.min.y + 20),
        z: Math.max(200, boundingBox.max.z - boundingBox.min.z + 20),
      }
    : { x: 200, y: 200, z: 200 };

  return (
    <div className="w-full h-full">
      <Canvas camera={{ position: [150, 150, 150], fov: 50 }}>
        <ambientLight intensity={0.5} />
        <pointLight position={[100, 100, 100]} />
        <pointLight position={[-100, 100, -100]} intensity={0.5} />

        <OrbitControls
          enablePan
          enableZoom
          enableRotate
          target={[0, buildSize.z / 2, 0]}
        />

        {/* Build platform */}
        <BuildPlatform size={buildSize} heated={material !== 'PLA'} />

        {/* Toolpath visualization */}
        {layerView && layers.length > 0 ? (
          <LayerView
            layers={layers}
            currentLayer={currentLayer}
            showTravel={showTravel}
            material={material}
          />
        ) : (
          <>
            <ToolPath
              segments={segments}
              currentSegment={animate ? animatedSegment : currentSegment}
              showTravel={showTravel}
              showRetractions={showRetractions}
              material={material}
            />

            {/* Material deposition (optional, for realistic view) */}
            {false && ( // Toggle for performance
              <MaterialDeposition
                segments={segments}
                currentSegment={animate ? animatedSegment : currentSegment}
                material={material}
              />
            )}
          </>
        )}

        {/* Print head */}
        {animate && (
          <PrintHead position={printHeadPos} isExtruding={isExtruding} />
        )}

        {/* Bounding box */}
        {boundingBox && (
          <Box
            position={[
              (boundingBox.max.x + boundingBox.min.x) / 2,
              (boundingBox.max.y + boundingBox.min.y) / 2,
              (boundingBox.max.z + boundingBox.min.z) / 2,
            ]}
            args={[
              boundingBox.max.x - boundingBox.min.x,
              boundingBox.max.y - boundingBox.min.y,
              boundingBox.max.z - boundingBox.min.z,
            ]}
          >
            <meshBasicMaterial color="#00ff00" wireframe opacity={0.2} transparent />
          </Box>
        )}
      </Canvas>
    </div>
  );
};