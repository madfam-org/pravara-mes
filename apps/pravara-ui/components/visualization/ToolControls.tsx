'use client';

import { useState } from 'react';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Label } from '@/components/ui/label';
import { Slider } from '@/components/ui/slider';
import { Switch } from '@/components/ui/switch';
import { Button } from '@/components/ui/button';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { Input } from '@/components/ui/input';
import {
  Thermometer,
  Gauge,
  Zap,
  RotateCw,
  Move3d,
  Layers,
  Wind,
  Droplets,
  Settings,
  Play,
  Pause,
  Square,
  Home
} from 'lucide-react';

// Tool mode selector component
interface ToolModeSelectorProps {
  currentTool: string;
  availableTools: string[];
  onToolChange: (tool: string) => void;
}

export function ToolModeSelector({ currentTool, availableTools, onToolChange }: ToolModeSelectorProps) {
  const toolIcons: Record<string, React.ReactNode> = {
    '3d_printing': <Layers className="h-4 w-4" />,
    'laser': <Zap className="h-4 w-4" />,
    'cnc': <Settings className="h-4 w-4" />,
    'pen_plotter': <Move3d className="h-4 w-4" />,
  };

  return (
    <Card>
      <CardHeader>
        <CardTitle>Tool Mode</CardTitle>
        <CardDescription>Select the active tool head</CardDescription>
      </CardHeader>
      <CardContent>
        <div className="grid grid-cols-2 gap-2">
          {availableTools.map((tool) => (
            <Button
              key={tool}
              variant={currentTool === tool ? 'default' : 'outline'}
              onClick={() => onToolChange(tool)}
              className="flex items-center gap-2"
            >
              {toolIcons[tool]}
              <span>{tool.replace('_', ' ').toUpperCase()}</span>
            </Button>
          ))}
        </div>
      </CardContent>
    </Card>
  );
}

// 3D Printing controls
interface PrintingControlsProps {
  onSettingsChange: (settings: any) => void;
}

export function PrintingControls({ onSettingsChange }: PrintingControlsProps) {
  const [settings, setSettings] = useState({
    nozzleTemp: 210,
    bedTemp: 60,
    printSpeed: 50,
    layerHeight: 0.2,
    infillPercent: 20,
    fanSpeed: 100,
    retractDistance: 1.5,
    retractSpeed: 40,
  });

  const updateSetting = (key: string, value: number) => {
    const newSettings = { ...settings, [key]: value };
    setSettings(newSettings);
    onSettingsChange(newSettings);
  };

  return (
    <Card>
      <CardHeader>
        <CardTitle>3D Printing Controls</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        {/* Temperature controls */}
        <div className="space-y-2">
          <div className="flex items-center gap-2">
            <Thermometer className="h-4 w-4" />
            <Label>Nozzle Temperature: {settings.nozzleTemp}°C</Label>
          </div>
          <Slider
            value={[settings.nozzleTemp]}
            onValueChange={([v]) => updateSetting('nozzleTemp', v)}
            min={180}
            max={280}
            step={5}
          />
        </div>

        <div className="space-y-2">
          <div className="flex items-center gap-2">
            <Thermometer className="h-4 w-4" />
            <Label>Bed Temperature: {settings.bedTemp}°C</Label>
          </div>
          <Slider
            value={[settings.bedTemp]}
            onValueChange={([v]) => updateSetting('bedTemp', v)}
            min={0}
            max={110}
            step={5}
          />
        </div>

        {/* Speed controls */}
        <div className="space-y-2">
          <div className="flex items-center gap-2">
            <Gauge className="h-4 w-4" />
            <Label>Print Speed: {settings.printSpeed} mm/s</Label>
          </div>
          <Slider
            value={[settings.printSpeed]}
            onValueChange={([v]) => updateSetting('printSpeed', v)}
            min={10}
            max={200}
            step={5}
          />
        </div>

        {/* Layer settings */}
        <div className="space-y-2">
          <Label>Layer Height: {settings.layerHeight} mm</Label>
          <Select
            value={settings.layerHeight.toString()}
            onValueChange={(v) => updateSetting('layerHeight', parseFloat(v))}
          >
            <SelectTrigger>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="0.1">0.1 mm (Fine)</SelectItem>
              <SelectItem value="0.15">0.15 mm</SelectItem>
              <SelectItem value="0.2">0.2 mm (Standard)</SelectItem>
              <SelectItem value="0.25">0.25 mm</SelectItem>
              <SelectItem value="0.3">0.3 mm (Draft)</SelectItem>
            </SelectContent>
          </Select>
        </div>

        {/* Infill */}
        <div className="space-y-2">
          <Label>Infill Density: {settings.infillPercent}%</Label>
          <Slider
            value={[settings.infillPercent]}
            onValueChange={([v]) => updateSetting('infillPercent', v)}
            min={0}
            max={100}
            step={5}
          />
        </div>

        {/* Cooling */}
        <div className="space-y-2">
          <div className="flex items-center gap-2">
            <Wind className="h-4 w-4" />
            <Label>Fan Speed: {settings.fanSpeed}%</Label>
          </div>
          <Slider
            value={[settings.fanSpeed]}
            onValueChange={([v]) => updateSetting('fanSpeed', v)}
            min={0}
            max={100}
            step={10}
          />
        </div>
      </CardContent>
    </Card>
  );
}

// Laser controls
interface LaserControlsProps {
  onSettingsChange: (settings: any) => void;
}

export function LaserControls({ onSettingsChange }: LaserControlsProps) {
  const [settings, setSettings] = useState({
    power: 50,
    speed: 1000,
    passes: 1,
    zOffset: 0,
    airAssist: true,
    pulsed: false,
    frequency: 1000,
  });

  const updateSetting = (key: string, value: any) => {
    const newSettings = { ...settings, [key]: value };
    setSettings(newSettings);
    onSettingsChange(newSettings);
  };

  return (
    <Card>
      <CardHeader>
        <CardTitle>Laser Controls</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        {/* Power control */}
        <div className="space-y-2">
          <div className="flex items-center gap-2">
            <Zap className="h-4 w-4" />
            <Label>Laser Power: {settings.power}%</Label>
          </div>
          <Slider
            value={[settings.power]}
            onValueChange={([v]) => updateSetting('power', v)}
            min={0}
            max={100}
            step={5}
            className="accent-red-500"
          />
        </div>

        {/* Speed control */}
        <div className="space-y-2">
          <div className="flex items-center gap-2">
            <Gauge className="h-4 w-4" />
            <Label>Movement Speed: {settings.speed} mm/min</Label>
          </div>
          <Slider
            value={[settings.speed]}
            onValueChange={([v]) => updateSetting('speed', v)}
            min={100}
            max={6000}
            step={100}
          />
        </div>

        {/* Passes */}
        <div className="space-y-2">
          <Label>Number of Passes: {settings.passes}</Label>
          <Slider
            value={[settings.passes]}
            onValueChange={([v]) => updateSetting('passes', v)}
            min={1}
            max={10}
            step={1}
          />
        </div>

        {/* Z Offset */}
        <div className="space-y-2">
          <Label>Z Offset: {settings.zOffset} mm</Label>
          <Slider
            value={[settings.zOffset]}
            onValueChange={([v]) => updateSetting('zOffset', v)}
            min={-5}
            max={5}
            step={0.1}
          />
        </div>

        {/* Air assist */}
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <Wind className="h-4 w-4" />
            <Label>Air Assist</Label>
          </div>
          <Switch
            checked={settings.airAssist}
            onCheckedChange={(v) => updateSetting('airAssist', v)}
          />
        </div>

        {/* Pulsed mode */}
        <div className="flex items-center justify-between">
          <Label>Pulsed Mode</Label>
          <Switch
            checked={settings.pulsed}
            onCheckedChange={(v) => updateSetting('pulsed', v)}
          />
        </div>

        {settings.pulsed && (
          <div className="space-y-2">
            <Label>Pulse Frequency: {settings.frequency} Hz</Label>
            <Slider
              value={[settings.frequency]}
              onValueChange={([v]) => updateSetting('frequency', v)}
              min={100}
              max={10000}
              step={100}
            />
          </div>
        )}
      </CardContent>
    </Card>
  );
}

// CNC controls
interface CNCControlsProps {
  onSettingsChange: (settings: any) => void;
}

export function CNCControls({ onSettingsChange }: CNCControlsProps) {
  const [settings, setSettings] = useState({
    spindleSpeed: 10000,
    feedRate: 500,
    plungeRate: 100,
    depthPerPass: 1,
    totalDepth: 5,
    coolant: false,
    climbMilling: false,
    toolDiameter: 3.175,
    toolType: 'end_mill',
  });

  const updateSetting = (key: string, value: any) => {
    const newSettings = { ...settings, [key]: value };
    setSettings(newSettings);
    onSettingsChange(newSettings);
  };

  return (
    <Card>
      <CardHeader>
        <CardTitle>CNC Controls</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        {/* Spindle speed */}
        <div className="space-y-2">
          <div className="flex items-center gap-2">
            <RotateCw className="h-4 w-4" />
            <Label>Spindle Speed: {settings.spindleSpeed} RPM</Label>
          </div>
          <Slider
            value={[settings.spindleSpeed]}
            onValueChange={([v]) => updateSetting('spindleSpeed', v)}
            min={1000}
            max={30000}
            step={500}
          />
        </div>

        {/* Feed rate */}
        <div className="space-y-2">
          <Label>Feed Rate: {settings.feedRate} mm/min</Label>
          <Slider
            value={[settings.feedRate]}
            onValueChange={([v]) => updateSetting('feedRate', v)}
            min={50}
            max={3000}
            step={50}
          />
        </div>

        {/* Plunge rate */}
        <div className="space-y-2">
          <Label>Plunge Rate: {settings.plungeRate} mm/min</Label>
          <Slider
            value={[settings.plungeRate]}
            onValueChange={([v]) => updateSetting('plungeRate', v)}
            min={10}
            max={500}
            step={10}
          />
        </div>

        {/* Depth per pass */}
        <div className="space-y-2">
          <Label>Depth per Pass: {settings.depthPerPass} mm</Label>
          <Slider
            value={[settings.depthPerPass]}
            onValueChange={([v]) => updateSetting('depthPerPass', v)}
            min={0.1}
            max={5}
            step={0.1}
          />
        </div>

        {/* Total depth */}
        <div className="space-y-2">
          <Label>Total Depth: {settings.totalDepth} mm</Label>
          <Slider
            value={[settings.totalDepth]}
            onValueChange={([v]) => updateSetting('totalDepth', v)}
            min={0.5}
            max={20}
            step={0.5}
          />
        </div>

        {/* Tool selection */}
        <div className="space-y-2">
          <Label>Tool Type</Label>
          <Select
            value={settings.toolType}
            onValueChange={(v) => updateSetting('toolType', v)}
          >
            <SelectTrigger>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="end_mill">End Mill</SelectItem>
              <SelectItem value="ball_nose">Ball Nose</SelectItem>
              <SelectItem value="v_bit">V-Bit</SelectItem>
              <SelectItem value="drill">Drill Bit</SelectItem>
              <SelectItem value="engraving">Engraving Bit</SelectItem>
            </SelectContent>
          </Select>
        </div>

        {/* Tool diameter */}
        <div className="space-y-2">
          <Label>Tool Diameter: {settings.toolDiameter} mm</Label>
          <Select
            value={settings.toolDiameter.toString()}
            onValueChange={(v) => updateSetting('toolDiameter', parseFloat(v))}
          >
            <SelectTrigger>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="1">1.0 mm</SelectItem>
              <SelectItem value="2">2.0 mm</SelectItem>
              <SelectItem value="3.175">3.175 mm (1/8")</SelectItem>
              <SelectItem value="6">6.0 mm</SelectItem>
              <SelectItem value="6.35">6.35 mm (1/4")</SelectItem>
            </SelectContent>
          </Select>
        </div>

        {/* Coolant */}
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <Droplets className="h-4 w-4" />
            <Label>Coolant</Label>
          </div>
          <Switch
            checked={settings.coolant}
            onCheckedChange={(v) => updateSetting('coolant', v)}
          />
        </div>

        {/* Climb milling */}
        <div className="flex items-center justify-between">
          <Label>Climb Milling</Label>
          <Switch
            checked={settings.climbMilling}
            onCheckedChange={(v) => updateSetting('climbMilling', v)}
          />
        </div>
      </CardContent>
    </Card>
  );
}

// Main control panel with all tools
interface MultiToolControlPanelProps {
  currentTool: string;
  availableTools: string[];
  onToolChange: (tool: string) => void;
  onSettingsChange: (tool: string, settings: any) => void;
  onCommand: (command: string) => void;
}

export default function MultiToolControlPanel({
  currentTool,
  availableTools,
  onToolChange,
  onSettingsChange,
  onCommand,
}: MultiToolControlPanelProps) {
  return (
    <div className="space-y-4">
      {/* Tool selector */}
      <ToolModeSelector
        currentTool={currentTool}
        availableTools={availableTools}
        onToolChange={onToolChange}
      />

      {/* Machine controls */}
      <Card>
        <CardHeader>
          <CardTitle>Machine Controls</CardTitle>
        </CardHeader>
        <CardContent className="flex gap-2">
          <Button onClick={() => onCommand('home')} variant="outline" size="sm">
            <Home className="h-4 w-4 mr-1" />
            Home
          </Button>
          <Button onClick={() => onCommand('play')} variant="outline" size="sm">
            <Play className="h-4 w-4 mr-1" />
            Start
          </Button>
          <Button onClick={() => onCommand('pause')} variant="outline" size="sm">
            <Pause className="h-4 w-4 mr-1" />
            Pause
          </Button>
          <Button onClick={() => onCommand('stop')} variant="outline" size="sm">
            <Square className="h-4 w-4 mr-1" />
            Stop
          </Button>
        </CardContent>
      </Card>

      {/* Tool-specific controls */}
      <Tabs value={currentTool} onValueChange={onToolChange}>
        <TabsList className="grid grid-cols-3 w-full">
          {availableTools.includes('3d_printing') && (
            <TabsTrigger value="3d_printing">3D Printing</TabsTrigger>
          )}
          {availableTools.includes('laser') && (
            <TabsTrigger value="laser">Laser</TabsTrigger>
          )}
          {availableTools.includes('cnc') && (
            <TabsTrigger value="cnc">CNC</TabsTrigger>
          )}
        </TabsList>

        <TabsContent value="3d_printing">
          <PrintingControls
            onSettingsChange={(settings) => onSettingsChange('3d_printing', settings)}
          />
        </TabsContent>

        <TabsContent value="laser">
          <LaserControls
            onSettingsChange={(settings) => onSettingsChange('laser', settings)}
          />
        </TabsContent>

        <TabsContent value="cnc">
          <CNCControls
            onSettingsChange={(settings) => onSettingsChange('cnc', settings)}
          />
        </TabsContent>
      </Tabs>
    </div>
  );
}