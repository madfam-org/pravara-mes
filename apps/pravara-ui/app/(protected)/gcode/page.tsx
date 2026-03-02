'use client';

import { useState, useCallback, useEffect } from 'react';
import { GCodeVisualization } from '@/components/gcode/GCodeVisualization';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { Slider } from '@/components/ui/slider';
import { Switch } from '@/components/ui/switch';
import { Label } from '@/components/ui/label';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Upload, Play, Pause, RotateCcw, Download, Layers, Box, Eye, EyeOff } from 'lucide-react';
import { useToast } from '@/components/ui/use-toast';

interface SimulationResult {
  segments: any[];
  layers: any[];
  bounding_box: {
    min: { x: number; y: number; z: number };
    max: { x: number; y: number; z: number };
  };
  stats: {
    layer_count: number;
    print_time_min: number;
    filament_meters: number;
    weight_grams: number;
    volume_cm3: number;
  };
  material: string;
}

export default function GCodePage() {
  const { toast } = useToast();

  // State
  const [gcodeFile, setGcodeFile] = useState<File | null>(null);
  const [gcodeContent, setGcodeContent] = useState<string>('');
  const [simulationResult, setSimulationResult] = useState<SimulationResult | null>(null);
  const [isLoading, setIsLoading] = useState(false);
  const [isAnimating, setIsAnimating] = useState(false);
  const [currentSegment, setCurrentSegment] = useState(0);
  const [currentLayer, setCurrentLayer] = useState(0);

  // Visualization settings
  const [showTravel, setShowTravel] = useState(true);
  const [showRetractions, setShowRetractions] = useState(false);
  const [layerView, setLayerView] = useState(false);
  const [material, setMaterial] = useState('PLA');
  const [speedMultiplier, setSpeedMultiplier] = useState(1);

  // Handle file upload
  const handleFileUpload = useCallback((event: React.ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0];
    if (!file) return;

    if (!file.name.endsWith('.gcode') && !file.name.endsWith('.gco')) {
      toast({
        title: 'Invalid file type',
        description: 'Please upload a .gcode or .gco file',
        variant: 'destructive',
      });
      return;
    }

    setGcodeFile(file);

    // Read file content
    const reader = new FileReader();
    reader.onload = (e) => {
      const content = e.target?.result as string;
      setGcodeContent(content);

      // Auto-simulate on upload
      simulateGCode(content);
    };
    reader.readAsText(file);
  }, []);

  // Simulate G-code
  const simulateGCode = async (gcode: string) => {
    setIsLoading(true);

    try {
      // Send to visualization engine for simulation
      const response = await fetch('/api/visualization/simulate-gcode', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          gcode,
          material,
          nozzle_diameter: 0.4,
        }),
      });

      if (!response.ok) throw new Error('Simulation failed');

      const result = await response.json();
      setSimulationResult(result);

      toast({
        title: 'Simulation complete',
        description: `${result.stats.layer_count} layers, ${result.stats.print_time_min.toFixed(1)} minutes`,
      });
    } catch (error) {
      console.error('Simulation error:', error);

      // For demo, use mock data
      const mockResult: SimulationResult = {
        segments: generateMockSegments(),
        layers: generateMockLayers(),
        bounding_box: {
          min: { x: -50, y: -50, z: 0 },
          max: { x: 50, y: 50, z: 100 },
        },
        stats: {
          layer_count: 500,
          print_time_min: 180,
          filament_meters: 12.5,
          weight_grams: 35.2,
          volume_cm3: 28.2,
        },
        material: material,
      };

      setSimulationResult(mockResult);
    } finally {
      setIsLoading(false);
    }
  };

  // Generate mock segments for demo
  const generateMockSegments = () => {
    const segments = [];
    const layers = 100;
    const pointsPerLayer = 50;

    for (let layer = 0; layer < layers; layer++) {
      const z = layer * 0.2;
      for (let i = 0; i < pointsPerLayer; i++) {
        const angle = (i / pointsPerLayer) * Math.PI * 2;
        const radius = 30 + Math.sin(layer * 0.1) * 10;

        const nextAngle = ((i + 1) / pointsPerLayer) * Math.PI * 2;
        const nextRadius = 30 + Math.sin(layer * 0.1) * 10;

        segments.push({
          start: {
            x: Math.cos(angle) * radius,
            y: Math.sin(angle) * radius,
            z: z,
          },
          end: {
            x: Math.cos(nextAngle) * nextRadius,
            y: Math.sin(nextAngle) * nextRadius,
            z: z,
          },
          extrusion_rate: 5,
          layer_height: 0.2,
          line_width: 0.4,
          temperature: 210,
          speed: 50,
          material: 'PLA',
          is_retraction: false,
          is_prime: false,
          is_travel: i % 10 === 0,
          volume_deposited: 0.1,
        });
      }
    }

    return segments;
  };

  // Generate mock layers
  const generateMockLayers = () => {
    const layers = [];
    for (let i = 0; i < 100; i++) {
      layers.push({
        number: i + 1,
        height: i * 0.2,
        segments: [],
        print_time: 108,
        filament_used_mm: 125,
      });
    }
    return layers;
  };

  // Animation control
  const toggleAnimation = () => {
    setIsAnimating(!isAnimating);
  };

  const resetAnimation = () => {
    setCurrentSegment(0);
    setCurrentLayer(0);
    setIsAnimating(false);
  };

  return (
    <div className="flex h-[calc(100vh-4rem)] gap-4 p-4">
      {/* Left panel - Controls */}
      <div className="w-96 space-y-4 overflow-y-auto">
        {/* File upload */}
        <Card>
          <CardHeader>
            <CardTitle>G-Code File</CardTitle>
            <CardDescription>
              Upload FullControl or standard G-code files for visualization
            </CardDescription>
          </CardHeader>
          <CardContent>
            <div className="space-y-4">
              <div className="flex items-center gap-2">
                <Button
                  variant="outline"
                  className="w-full"
                  onClick={() => document.getElementById('gcode-upload')?.click()}
                >
                  <Upload className="mr-2 h-4 w-4" />
                  Upload G-Code
                </Button>
                <input
                  id="gcode-upload"
                  type="file"
                  accept=".gcode,.gco"
                  className="hidden"
                  onChange={handleFileUpload}
                />
              </div>

              {gcodeFile && (
                <div className="text-sm text-muted-foreground">
                  File: {gcodeFile.name}
                </div>
              )}

              <Select value={material} onValueChange={setMaterial}>
                <SelectTrigger>
                  <SelectValue placeholder="Select material" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="PLA">PLA</SelectItem>
                  <SelectItem value="ABS">ABS</SelectItem>
                  <SelectItem value="PETG">PETG</SelectItem>
                  <SelectItem value="TPU">TPU</SelectItem>
                </SelectContent>
              </Select>
            </div>
          </CardContent>
        </Card>

        {/* Statistics */}
        {simulationResult && (
          <Card>
            <CardHeader>
              <CardTitle>Print Statistics</CardTitle>
            </CardHeader>
            <CardContent>
              <div className="grid grid-cols-2 gap-2 text-sm">
                <div>Layers:</div>
                <div className="font-mono">{simulationResult.stats.layer_count}</div>

                <div>Print Time:</div>
                <div className="font-mono">{simulationResult.stats.print_time_min.toFixed(1)} min</div>

                <div>Filament:</div>
                <div className="font-mono">{simulationResult.stats.filament_meters.toFixed(2)} m</div>

                <div>Weight:</div>
                <div className="font-mono">{simulationResult.stats.weight_grams.toFixed(1)} g</div>

                <div>Volume:</div>
                <div className="font-mono">{simulationResult.stats.volume_cm3.toFixed(1)} cm³</div>
              </div>
            </CardContent>
          </Card>
        )}

        {/* Visualization controls */}
        <Card>
          <CardHeader>
            <CardTitle>Visualization</CardTitle>
          </CardHeader>
          <CardContent className="space-y-4">
            {/* Animation controls */}
            <div className="flex gap-2">
              <Button
                size="sm"
                variant={isAnimating ? 'destructive' : 'default'}
                onClick={toggleAnimation}
              >
                {isAnimating ? <Pause className="h-4 w-4" /> : <Play className="h-4 w-4" />}
              </Button>
              <Button size="sm" variant="outline" onClick={resetAnimation}>
                <RotateCcw className="h-4 w-4" />
              </Button>
            </div>

            {/* Speed control */}
            <div className="space-y-2">
              <Label>Animation Speed: {speedMultiplier}x</Label>
              <Slider
                value={[speedMultiplier]}
                onValueChange={([v]) => setSpeedMultiplier(v)}
                min={0.1}
                max={10}
                step={0.1}
              />
            </div>

            {/* View options */}
            <div className="space-y-2">
              <div className="flex items-center space-x-2">
                <Switch
                  id="layer-view"
                  checked={layerView}
                  onCheckedChange={setLayerView}
                />
                <Label htmlFor="layer-view">Layer View</Label>
              </div>

              {layerView && simulationResult && (
                <div className="space-y-2">
                  <Label>Layer: {currentLayer + 1}/{simulationResult.layers.length}</Label>
                  <Slider
                    value={[currentLayer]}
                    onValueChange={([v]) => setCurrentLayer(v)}
                    min={0}
                    max={simulationResult.layers.length - 1}
                    step={1}
                  />
                </div>
              )}

              <div className="flex items-center space-x-2">
                <Switch
                  id="show-travel"
                  checked={showTravel}
                  onCheckedChange={setShowTravel}
                />
                <Label htmlFor="show-travel">Show Travel Moves</Label>
              </div>

              <div className="flex items-center space-x-2">
                <Switch
                  id="show-retractions"
                  checked={showRetractions}
                  onCheckedChange={setShowRetractions}
                />
                <Label htmlFor="show-retractions">Show Retractions</Label>
              </div>
            </div>
          </CardContent>
        </Card>

        {/* Export options */}
        <Card>
          <CardHeader>
            <CardTitle>Export</CardTitle>
          </CardHeader>
          <CardContent>
            <Button variant="outline" className="w-full">
              <Download className="mr-2 h-4 w-4" />
              Export to STL
            </Button>
          </CardContent>
        </Card>
      </div>

      {/* Right panel - 3D Visualization */}
      <Card className="flex-1">
        <CardContent className="h-full p-0">
          {simulationResult ? (
            <GCodeVisualization
              segments={simulationResult.segments}
              layers={simulationResult.layers}
              currentSegment={currentSegment}
              currentLayer={currentLayer}
              showTravel={showTravel}
              showRetractions={showRetractions}
              layerView={layerView}
              material={material}
              boundingBox={simulationResult.bounding_box}
              animate={isAnimating}
              speedMultiplier={speedMultiplier}
            />
          ) : (
            <div className="flex h-full items-center justify-center text-muted-foreground">
              <div className="text-center">
                <Box className="mx-auto h-12 w-12 mb-4" />
                <p>Upload a G-code file to visualize</p>
              </div>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}