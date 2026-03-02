'use client';

import { useRef, useEffect, useState } from 'react';
import { Canvas, useFrame, useThree } from '@react-three/fiber';
import { OrbitControls, Grid, Line, Text, Box, Cylinder, Cone, Sphere } from '@react-three/drei';
import * as THREE from 'three';

// Tool type definitions
export type ToolType = '3d_printing' | 'laser' | 'cnc' | 'pen_plotter';

interface ToolHeadProps {
  toolType: ToolType;
  position: [number, number, number];
  isActive: boolean;
}

// Tool head visualization component
function ToolHead({ toolType, position, isActive }: ToolHeadProps) {
  const meshRef = useRef<THREE.Group>(null);

  useFrame((state) => {
    if (meshRef.current && isActive) {
      // Subtle animation when active
      meshRef.current.rotation.z = Math.sin(state.clock.elapsedTime * 2) * 0.05;
    }
  });

  return (
    <group ref={meshRef} position={position}>
      {toolType === '3d_printing' && (
        <group>
          {/* Extruder body */}
          <Box args={[2, 2, 3]} position={[0, 0, 1.5]}>
            <meshStandardMaterial color="#444444" metalness={0.8} roughness={0.2} />
          </Box>
          {/* Nozzle */}
          <Cone args={[0.5, 1.5, 8]} position={[0, 0, -0.5]} rotation={[Math.PI, 0, 0]}>
            <meshStandardMaterial color="#ffaa00" metalness={0.9} roughness={0.1} />
          </Cone>
          {/* Heater block */}
          <Box args={[1.5, 1.5, 1]} position={[0, 0, 0]}>
            <meshStandardMaterial color="#cc0000" metalness={0.7} roughness={0.3} />
          </Box>
          {/* Cooling fan */}
          <Cylinder args={[1, 1, 0.3, 16]} position={[1.5, 0, 0.5]} rotation={[0, 0, Math.PI / 2]}>
            <meshStandardMaterial color="#0066cc" metalness={0.5} roughness={0.4} />
          </Cylinder>
        </group>
      )}

      {toolType === 'laser' && (
        <group>
          {/* Laser module */}
          <Cylinder args={[1, 1, 3, 16]} position={[0, 0, 1.5]}>
            <meshStandardMaterial color="#333333" metalness={0.9} roughness={0.1} />
          </Cylinder>
          {/* Lens housing */}
          <Cone args={[0.8, 1, 16]} position={[0, 0, -0.5]} rotation={[Math.PI, 0, 0]}>
            <meshStandardMaterial color="#666666" metalness={0.8} roughness={0.2} />
          </Cone>
          {/* Laser beam when active */}
          {isActive && (
            <Cylinder args={[0.05, 0.05, 10, 8]} position={[0, 0, -5]}>
              <meshBasicMaterial color="#ff0000" transparent opacity={0.8} />
            </Cylinder>
          )}
          {/* Safety shield */}
          <Box args={[3, 3, 0.1]} position={[0, 0, 0.5]}>
            <meshStandardMaterial color="#ffaa00" transparent opacity={0.3} />
          </Box>
        </group>
      )}

      {toolType === 'cnc' && (
        <group>
          {/* Spindle motor */}
          <Cylinder args={[1.2, 1.2, 4, 16]} position={[0, 0, 2]}>
            <meshStandardMaterial color="#2a2a2a" metalness={0.9} roughness={0.2} />
          </Cylinder>
          {/* Collet */}
          <Cone args={[0.8, 1.5, 8]} position={[0, 0, -0.5]} rotation={[Math.PI, 0, 0]}>
            <meshStandardMaterial color="#888888" metalness={0.95} roughness={0.05} />
          </Cone>
          {/* End mill / bit */}
          <Cylinder
            args={[0.2, 0.2, 3, 8]}
            position={[0, 0, -2]}
            rotation={isActive ? [0, 0, Date.now() * 0.01] : [0, 0, 0]}
          >
            <meshStandardMaterial color="#cccccc" metalness={1} roughness={0} />
          </Cylinder>
          {/* Dust shoe */}
          <Cylinder args={[2, 2.5, 0.5, 16]} position={[0, 0, 0]}>
            <meshStandardMaterial color="#666666" transparent opacity={0.7} />
          </Cylinder>
        </group>
      )}

      {toolType === 'pen_plotter' && (
        <group>
          {/* Pen holder */}
          <Cylinder args={[0.5, 0.5, 3, 16]} position={[0, 0, 1.5]}>
            <meshStandardMaterial color="#0066cc" metalness={0.3} roughness={0.7} />
          </Cylinder>
          {/* Pen */}
          <Cylinder args={[0.15, 0.15, 4, 8]} position={[0, 0, -1]}>
            <meshStandardMaterial color="#000000" metalness={0.2} roughness={0.8} />
          </Cylinder>
          {/* Pen tip */}
          <Cone args={[0.15, 0.5, 8]} position={[0, 0, -3]} rotation={[Math.PI, 0, 0]}>
            <meshStandardMaterial color="#333333" metalness={0.1} roughness={0.9} />
          </Cone>
        </group>
      )}

      {/* Position indicator */}
      <Sphere args={[0.2, 16, 16]} position={[0, 0, 0]}>
        <meshBasicMaterial color={isActive ? '#00ff00' : '#ffff00'} />
      </Sphere>
    </group>
  );
}

// Material visualization for additive/subtractive operations
interface MaterialVisualizationProps {
  type: 'additive' | 'subtractive';
  material: string;
  dimensions: [number, number, number];
}

function MaterialVisualization({ type, material, dimensions }: MaterialVisualizationProps) {
  const meshRef = useRef<THREE.Mesh>(null);

  // Material colors based on type
  const materialColors: Record<string, string> = {
    PLA: '#4CAF50',
    ABS: '#FF5722',
    PETG: '#2196F3',
    TPU: '#9C27B0',
    Nylon: '#607D8B',
    Wood: '#8D6E63',
    Metal: '#9E9E9E',
    Acrylic: '#00BCD4',
    MDF: '#795548',
    Aluminum: '#B0BEC5',
    Steel: '#546E7A',
  };

  const color = materialColors[material] || '#888888';

  if (type === 'additive') {
    // Show printed layers
    return (
      <group>
        {Array.from({ length: 10 }, (_, i) => (
          <Box
            key={i}
            args={[dimensions[0], dimensions[1], 0.2]}
            position={[0, 0, i * 0.2]}
          >
            <meshStandardMaterial
              color={color}
              transparent
              opacity={0.8}
              metalness={0.3}
              roughness={0.7}
            />
          </Box>
        ))}
      </group>
    );
  }

  // Subtractive - show material block with cuts
  return (
    <Box ref={meshRef} args={dimensions}>
      <meshStandardMaterial
        color={color}
        metalness={0.4}
        roughness={0.6}
      />
    </Box>
  );
}

// Tool path visualization
interface ToolPathProps {
  points: [number, number, number][];
  toolType: ToolType;
  isActive: boolean;
}

function ToolPath({ points, toolType, isActive }: ToolPathProps) {
  const colors: Record<ToolType, string> = {
    '3d_printing': '#00ff00',
    'laser': '#ff0000',
    'cnc': '#0088ff',
    'pen_plotter': '#ff00ff',
  };

  if (points.length < 2) return null;

  return (
    <Line
      points={points}
      color={colors[toolType]}
      lineWidth={2}
      dashed={!isActive}
      dashScale={5}
      transparent
      opacity={isActive ? 1 : 0.5}
    />
  );
}

// Main multi-tool visualization component
interface MultiToolVisualizationProps {
  toolType: ToolType;
  isActive: boolean;
  position: { x: number; y: number; z: number };
  toolPath?: [number, number, number][];
  material?: string;
  workpieceSize?: [number, number, number];
  showGrid?: boolean;
  showAxes?: boolean;
  cameraPosition?: [number, number, number];
}

export default function MultiToolVisualization({
  toolType,
  isActive,
  position,
  toolPath = [],
  material = 'PLA',
  workpieceSize = [200, 200, 100],
  showGrid = true,
  showAxes = true,
  cameraPosition = [150, 150, 150],
}: MultiToolVisualizationProps) {
  const [currentPath, setCurrentPath] = useState<[number, number, number][]>([]);

  useEffect(() => {
    // Animate path drawing
    if (toolPath.length > 0 && isActive) {
      let index = 0;
      const interval = setInterval(() => {
        if (index < toolPath.length) {
          setCurrentPath(toolPath.slice(0, index + 1));
          index++;
        } else {
          clearInterval(interval);
        }
      }, 50);
      return () => clearInterval(interval);
    }
  }, [toolPath, isActive]);

  const isSubtractive = toolType === 'cnc' || toolType === 'laser';

  return (
    <div className="w-full h-full">
      <Canvas
        shadows
        camera={{ position: cameraPosition, fov: 50 }}
        gl={{ preserveDrawingBuffer: true }}
      >
        <ambientLight intensity={0.5} />
        <directionalLight
          position={[10, 10, 5]}
          intensity={1}
          castShadow
          shadow-mapSize-width={2048}
          shadow-mapSize-height={2048}
        />
        <pointLight position={[-10, -10, -5]} intensity={0.5} />

        <OrbitControls
          enablePan
          enableZoom
          enableRotate
          maxPolarAngle={Math.PI * 0.9}
          minDistance={50}
          maxDistance={500}
        />

        {/* Grid */}
        {showGrid && (
          <Grid
            args={[workpieceSize[0], workpieceSize[1]]}
            cellSize={10}
            cellThickness={0.5}
            cellColor="#666666"
            sectionSize={50}
            sectionThickness={1}
            sectionColor="#888888"
            fadeDistance={400}
            fadeStrength={1}
            position={[0, 0, 0]}
            rotation={[Math.PI / 2, 0, 0]}
          />
        )}

        {/* Axes */}
        {showAxes && (
          <group>
            {/* X axis - Red */}
            <Line points={[[0, 0, 0], [workpieceSize[0] / 2 + 20, 0, 0]]} color="#ff0000" lineWidth={2} />
            <Text position={[workpieceSize[0] / 2 + 30, 0, 0]} color="#ff0000" fontSize={10}>
              X
            </Text>

            {/* Y axis - Green */}
            <Line points={[[0, 0, 0], [0, workpieceSize[1] / 2 + 20, 0]]} color="#00ff00" lineWidth={2} />
            <Text position={[0, workpieceSize[1] / 2 + 30, 0]} color="#00ff00" fontSize={10}>
              Y
            </Text>

            {/* Z axis - Blue */}
            <Line points={[[0, 0, 0], [0, 0, workpieceSize[2] / 2 + 20]]} color="#0000ff" lineWidth={2} />
            <Text position={[0, 0, workpieceSize[2] / 2 + 30]} color="#0000ff" fontSize={10}>
              Z
            </Text>
          </group>
        )}

        {/* Build platform / workpiece */}
        <Box args={[workpieceSize[0], workpieceSize[1], 2]} position={[0, 0, -1]} receiveShadow>
          <meshStandardMaterial color="#333333" metalness={0.5} roughness={0.8} />
        </Box>

        {/* Material visualization */}
        {isSubtractive && (
          <MaterialVisualization
            type="subtractive"
            material={material}
            dimensions={[workpieceSize[0] * 0.8, workpieceSize[1] * 0.8, 20]}
          />
        )}

        {/* Tool head */}
        <ToolHead
          toolType={toolType}
          position={[position.x, position.y, position.z]}
          isActive={isActive}
        />

        {/* Tool path */}
        {currentPath.length > 0 && (
          <ToolPath
            points={currentPath}
            toolType={toolType}
            isActive={isActive}
          />
        )}

        {/* Printed material (for 3D printing) */}
        {toolType === '3d_printing' && currentPath.length > 0 && (
          <group>
            {currentPath.map((point, index) => {
              if (index === 0) return null;
              const prevPoint = currentPath[index - 1];
              const length = Math.sqrt(
                Math.pow(point[0] - prevPoint[0], 2) +
                Math.pow(point[1] - prevPoint[1], 2) +
                Math.pow(point[2] - prevPoint[2], 2)
              );
              const midPoint: [number, number, number] = [
                (point[0] + prevPoint[0]) / 2,
                (point[1] + prevPoint[1]) / 2,
                (point[2] + prevPoint[2]) / 2,
              ];
              const angle = Math.atan2(point[1] - prevPoint[1], point[0] - prevPoint[0]);

              return (
                <Box
                  key={index}
                  args={[length, 0.4, 0.2]}
                  position={midPoint}
                  rotation={[0, 0, angle]}
                >
                  <meshStandardMaterial
                    color={material === 'PLA' ? '#4CAF50' : '#FF5722'}
                    metalness={0.2}
                    roughness={0.8}
                  />
                </Box>
              );
            })}
          </group>
        )}

        {/* Status text */}
        <Text
          position={[0, workpieceSize[1] / 2 + 50, 0]}
          color="#ffffff"
          fontSize={12}
          anchorX="center"
        >
          {`Tool: ${toolType.replace('_', ' ').toUpperCase()} | Status: ${isActive ? 'Active' : 'Idle'}`}
        </Text>
      </Canvas>
    </div>
  );
}