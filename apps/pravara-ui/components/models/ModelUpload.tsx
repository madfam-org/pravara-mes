"use client";

import React, { useState, useCallback } from "react";
import { usePravaraSession } from "@/lib/auth";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Upload, FileBox, Loader2, Globe, Search, Check, Package } from "lucide-react";
import { useToast } from "@/components/ui/use-toast";
import {
  modelsAPI,
  yantra4dAPI,
  type MachineModel,
  type Yantra4DPreview,
  type Yantra4DParameter,
} from "@/lib/api";

type ImportSource = "file" | "yantra4d";

interface ModelUploadProps {
  onUploaded?: (model: MachineModel) => void;
}

const ACCEPTED_EXTENSIONS = ".gltf,.glb,.stl";

export const ModelUpload: React.FC<ModelUploadProps> = ({ onUploaded }) => {
  const { data: session } = usePravaraSession();
  const token = session?.accessToken as string | undefined;
  const { toast } = useToast();

  const [importSource, setImportSource] = useState<ImportSource>("file");
  const [file, setFile] = useState<File | null>(null);
  const [modelName, setModelName] = useState("");
  const [machineType, setMachineType] = useState("3d_printer_fdm");
  const [isUploading, setIsUploading] = useState(false);
  const [dragActive, setDragActive] = useState(false);

  // Yantra4D state
  const [y4dSlug, setY4dSlug] = useState("");
  const [y4dPreview, setY4dPreview] = useState<Yantra4DPreview | null>(null);
  const [y4dMode, setY4dMode] = useState("");
  const [y4dParams, setY4dParams] = useState<Record<string, unknown>>({});
  const [y4dMachineType, setY4dMachineType] = useState("3d_printer_fdm");
  const [isLoadingPreview, setIsLoadingPreview] = useState(false);
  const [isImporting, setIsImporting] = useState(false);
  const [importResult, setImportResult] = useState<{
    sku: string;
    productId: string;
    bomCount: number;
    hasWI: boolean;
  } | null>(null);

  const handleFileSelect = useCallback(
    (selectedFile: File) => {
      const ext = selectedFile.name.split(".").pop()?.toLowerCase();
      if (!ext || !["gltf", "glb", "stl"].includes(ext)) {
        toast({
          title: "Invalid file type",
          description: "Please upload a .gltf, .glb, or .stl file",
          variant: "destructive",
        });
        return;
      }
      setFile(selectedFile);
      if (!modelName) {
        setModelName(selectedFile.name.replace(/\.[^.]+$/, ""));
      }
    },
    [modelName, toast]
  );

  const handleDrop = useCallback(
    (e: React.DragEvent) => {
      e.preventDefault();
      setDragActive(false);
      const droppedFile = e.dataTransfer.files[0];
      if (droppedFile) handleFileSelect(droppedFile);
    },
    [handleFileSelect]
  );

  const handleUpload = async () => {
    if (!file || !token) return;

    setIsUploading(true);
    try {
      const model = await modelsAPI.upload(token, file);
      toast({
        title: "Model uploaded",
        description: `${model.name} has been uploaded successfully`,
      });
      onUploaded?.(model);
      setFile(null);
      setModelName("");
    } catch (err) {
      toast({
        title: "Upload failed",
        description: err instanceof Error ? err.message : "Unknown error",
        variant: "destructive",
      });
    } finally {
      setIsUploading(false);
    }
  };

  // Yantra4D preview
  const handlePreview = async () => {
    if (!y4dSlug.trim() || !token) return;

    setIsLoadingPreview(true);
    setY4dPreview(null);
    setImportResult(null);
    try {
      const preview = await yantra4dAPI.preview(token, y4dSlug.trim());
      setY4dPreview(preview);

      // Set defaults
      if (preview.preview.modes?.length > 0) {
        setY4dMode(preview.preview.modes[0].id);
      }
      const defaults: Record<string, unknown> = {};
      for (const param of preview.preview.parameters || []) {
        defaults[param.id] = param.default;
      }
      setY4dParams(defaults);

      // Auto-detect machine type from category
      if (preview.preview.category === "cnc_part") {
        setY4dMachineType("cnc_3axis");
      } else {
        setY4dMachineType("3d_printer_fdm");
      }
    } catch (err) {
      toast({
        title: "Preview failed",
        description: err instanceof Error ? err.message : "Could not fetch project from Yantra4D",
        variant: "destructive",
      });
    } finally {
      setIsLoadingPreview(false);
    }
  };

  // Yantra4D import
  const handleY4dImport = async () => {
    if (!y4dSlug.trim() || !token) return;

    setIsImporting(true);
    try {
      const result = await yantra4dAPI.import(token, {
        slug: y4dSlug.trim(),
        mode: y4dMode || undefined,
        parameters: y4dParams,
        machine_type: y4dMachineType,
      });

      setImportResult({
        sku: result.product_definition.sku,
        productId: result.product_definition.id,
        bomCount: result.bom_items?.length ?? 0,
        hasWI: !!result.work_instruction,
      });

      toast({
        title: "Import successful",
        description: `Created product ${result.product_definition.sku} with ${result.bom_items?.length ?? 0} BOM items`,
      });
    } catch (err) {
      toast({
        title: "Import failed",
        description: err instanceof Error ? err.message : "Unknown error",
        variant: "destructive",
      });
    } finally {
      setIsImporting(false);
    }
  };

  const updateParam = (id: string, value: unknown) => {
    setY4dParams((prev) => ({ ...prev, [id]: value }));
  };

  const renderParamInput = (param: Yantra4DParameter) => {
    const value = y4dParams[param.id];
    const label = param.label?.en || param.id;

    if (param.type === "boolean" || param.type === "bool") {
      return (
        <div key={param.id} className="flex items-center gap-2">
          <input
            type="checkbox"
            id={`param-${param.id}`}
            checked={!!value}
            onChange={(e) => updateParam(param.id, e.target.checked)}
            className="h-4 w-4"
          />
          <Label htmlFor={`param-${param.id}`} className="text-sm">{label}</Label>
        </div>
      );
    }

    if (param.options && param.options.length > 0) {
      return (
        <div key={param.id} className="space-y-1">
          <Label className="text-sm">{label}</Label>
          <Select value={String(value ?? "")} onValueChange={(v) => updateParam(param.id, v)}>
            <SelectTrigger className="h-8 text-sm">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {param.options.map((opt) => (
                <SelectItem key={opt} value={opt}>{opt}</SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      );
    }

    if (param.type === "int" || param.type === "float" || param.type === "number") {
      return (
        <div key={param.id} className="space-y-1">
          <Label className="text-sm">{label}</Label>
          <Input
            type="number"
            className="h-8 text-sm"
            value={String(value ?? "")}
            min={param.min}
            max={param.max}
            step={param.step ?? (param.type === "int" ? 1 : 0.1)}
            onChange={(e) => updateParam(param.id, param.type === "int" ? parseInt(e.target.value) : parseFloat(e.target.value))}
          />
        </div>
      );
    }

    // Default: text input
    return (
      <div key={param.id} className="space-y-1">
        <Label className="text-sm">{label}</Label>
        <Input
          className="h-8 text-sm"
          value={String(value ?? "")}
          onChange={(e) => updateParam(param.id, e.target.value)}
        />
      </div>
    );
  };

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <FileBox className="h-5 w-5" />
          Upload 3D Model
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        {/* Source selector */}
        <div className="flex gap-2">
          <Button
            variant={importSource === "file" ? "default" : "outline"}
            size="sm"
            onClick={() => setImportSource("file")}
          >
            <Upload className="mr-1 h-4 w-4" />
            File Upload
          </Button>
          <Button
            variant={importSource === "yantra4d" ? "default" : "outline"}
            size="sm"
            onClick={() => setImportSource("yantra4d")}
          >
            <Globe className="mr-1 h-4 w-4" />
            Yantra4D
          </Button>
        </div>

        {importSource === "yantra4d" && (
          <div className="space-y-4">
            {/* Slug input + preview button */}
            <div className="flex gap-2">
              <Input
                placeholder="Project slug (e.g. gridfinity)"
                value={y4dSlug}
                onChange={(e) => setY4dSlug(e.target.value)}
                onKeyDown={(e) => e.key === "Enter" && handlePreview()}
              />
              <Button
                size="sm"
                onClick={handlePreview}
                disabled={!y4dSlug.trim() || isLoadingPreview}
              >
                {isLoadingPreview ? (
                  <Loader2 className="h-4 w-4 animate-spin" />
                ) : (
                  <Search className="h-4 w-4" />
                )}
              </Button>
            </div>

            {/* Preview results */}
            {y4dPreview && (
              <>
                <div className="p-3 border rounded-lg bg-muted/30 space-y-2">
                  <div className="flex items-center justify-between">
                    <span className="font-medium">{y4dPreview.preview.name}</span>
                    <span className="text-xs text-muted-foreground">v{y4dPreview.preview.version}</span>
                  </div>
                  <p className="text-sm text-muted-foreground">{y4dPreview.preview.description}</p>
                  <div className="flex gap-3 text-xs text-muted-foreground">
                    <span>SKU: {y4dPreview.preview.sku}</span>
                    <span>BOM: {y4dPreview.preview.bom_count} items</span>
                    <span>Steps: {y4dPreview.preview.step_count}</span>
                  </div>
                </div>

                {/* Mode selector */}
                {y4dPreview.preview.modes?.length > 1 && (
                  <div className="space-y-1">
                    <Label className="text-sm">Mode</Label>
                    <Select value={y4dMode} onValueChange={setY4dMode}>
                      <SelectTrigger className="h-8 text-sm">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        {y4dPreview.preview.modes.map((m) => (
                          <SelectItem key={m.id} value={m.id}>{m.label?.en || m.id}</SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </div>
                )}

                {/* Parameter inputs */}
                {y4dPreview.preview.parameters?.length > 0 && (
                  <div className="space-y-2">
                    <Label className="text-sm font-medium">Parameters</Label>
                    <div className="grid grid-cols-2 gap-2">
                      {y4dPreview.preview.parameters.map(renderParamInput)}
                    </div>
                  </div>
                )}

                {/* Machine type */}
                <div className="space-y-1">
                  <Label className="text-sm">Target Machine</Label>
                  <Select value={y4dMachineType} onValueChange={setY4dMachineType}>
                    <SelectTrigger className="h-8 text-sm">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="3d_printer_fdm">3D Printer (FDM)</SelectItem>
                      <SelectItem value="cnc_3axis">CNC 3-Axis</SelectItem>
                      <SelectItem value="laser_cutter">Laser Cutter</SelectItem>
                      <SelectItem value="multi_tool">Multi-Tool (Snapmaker)</SelectItem>
                    </SelectContent>
                  </Select>
                </div>

                {/* Import button */}
                <Button
                  className="w-full"
                  onClick={handleY4dImport}
                  disabled={isImporting}
                >
                  {isImporting ? (
                    <>
                      <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                      Importing...
                    </>
                  ) : (
                    <>
                      <Package className="mr-2 h-4 w-4" />
                      Import from Yantra4D
                    </>
                  )}
                </Button>
              </>
            )}

            {/* Success state */}
            {importResult && (
              <div className="p-4 border rounded-lg bg-green-50 dark:bg-green-950/20 space-y-2">
                <div className="flex items-center gap-2 text-green-700 dark:text-green-400">
                  <Check className="h-5 w-5" />
                  <span className="font-medium">Import Complete</span>
                </div>
                <div className="text-sm space-y-1">
                  <p>Product: <span className="font-mono">{importResult.sku}</span></p>
                  <p>BOM Items: {importResult.bomCount}</p>
                  {importResult.hasWI && <p>Work instruction created</p>}
                </div>
                <a
                  href={`/products/${importResult.productId}`}
                  className="text-sm text-primary underline"
                >
                  View Product
                </a>
              </div>
            )}
          </div>
        )}

        {importSource === "file" && (
        <>
        {/* Drop zone */}
        <div
          className={`border-2 border-dashed rounded-lg p-8 text-center transition-colors ${
            dragActive
              ? "border-primary bg-primary/5"
              : "border-muted-foreground/25 hover:border-muted-foreground/50"
          }`}
          onDragOver={(e) => {
            e.preventDefault();
            setDragActive(true);
          }}
          onDragLeave={() => setDragActive(false)}
          onDrop={handleDrop}
        >
          {file ? (
            <div className="space-y-2">
              <FileBox className="h-8 w-8 mx-auto text-primary" />
              <p className="font-medium">{file.name}</p>
              <p className="text-sm text-muted-foreground">
                {(file.size / 1024 / 1024).toFixed(2)} MB
              </p>
              <Button
                variant="ghost"
                size="sm"
                onClick={() => setFile(null)}
              >
                Remove
              </Button>
            </div>
          ) : (
            <div className="space-y-2">
              <Upload className="h-8 w-8 mx-auto text-muted-foreground" />
              <p className="text-muted-foreground">
                Drag & drop a 3D model file here, or click to browse
              </p>
              <p className="text-xs text-muted-foreground">
                Supported: GLTF, GLB, STL
              </p>
              <Input
                type="file"
                accept={ACCEPTED_EXTENSIONS}
                className="max-w-xs mx-auto"
                onChange={(e) => {
                  const f = e.target.files?.[0];
                  if (f) handleFileSelect(f);
                }}
              />
            </div>
          )}
        </div>

        {/* Metadata fields */}
        {file && (
          <>
            <div className="space-y-2">
              <Label htmlFor="model-name">Model Name</Label>
              <Input
                id="model-name"
                value={modelName}
                onChange={(e) => setModelName(e.target.value)}
                placeholder="Enter model name"
              />
            </div>

            <div className="space-y-2">
              <Label htmlFor="machine-type">Machine Type</Label>
              <Select value={machineType} onValueChange={setMachineType}>
                <SelectTrigger id="machine-type">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="3d_printer_fdm">3D Printer (FDM)</SelectItem>
                  <SelectItem value="cnc_3axis">CNC 3-Axis</SelectItem>
                  <SelectItem value="laser_cutter">Laser Cutter</SelectItem>
                  <SelectItem value="generic">Generic</SelectItem>
                </SelectContent>
              </Select>
            </div>

            <Button
              className="w-full"
              onClick={handleUpload}
              disabled={isUploading || !file}
            >
              {isUploading ? (
                <>
                  <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                  Uploading...
                </>
              ) : (
                <>
                  <Upload className="mr-2 h-4 w-4" />
                  Upload Model
                </>
              )}
            </Button>
          </>
        )}
        </>
        )}
      </CardContent>
    </Card>
  );
};
