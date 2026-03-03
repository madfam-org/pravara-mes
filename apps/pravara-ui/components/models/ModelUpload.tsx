"use client";

import React, { useState, useCallback } from "react";
import { useSession } from "next-auth/react";
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
import { Upload, FileBox, Loader2, Globe } from "lucide-react";
import { useToast } from "@/components/ui/use-toast";
import { modelsAPI, type MachineModel } from "@/lib/api";

type ImportSource = "file" | "yantra4d";

interface ModelUploadProps {
  onUploaded?: (model: MachineModel) => void;
}

const ACCEPTED_EXTENSIONS = ".gltf,.glb,.stl";

export const ModelUpload: React.FC<ModelUploadProps> = ({ onUploaded }) => {
  const { data: session } = useSession();
  const token = session?.accessToken as string | undefined;
  const { toast } = useToast();

  const [importSource, setImportSource] = useState<ImportSource>("file");
  const [file, setFile] = useState<File | null>(null);
  const [modelName, setModelName] = useState("");
  const [machineType, setMachineType] = useState("3d_printer_fdm");
  const [isUploading, setIsUploading] = useState(false);
  const [dragActive, setDragActive] = useState(false);

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
          <div className="p-6 text-center border rounded-lg bg-muted/50">
            <Globe className="h-8 w-8 mx-auto mb-2 text-muted-foreground" />
            <p className="font-medium">Yantra4D Integration</p>
            <p className="text-sm text-muted-foreground mt-1">
              Import configured hyperobjects directly from Yantra4D.
            </p>
            <p className="text-xs text-muted-foreground mt-2">
              Coming soon — awaiting API integration.
            </p>
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
