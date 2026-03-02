"use client";

import { LayoutGrid, List } from "lucide-react";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

export type ViewMode = "grid" | "table";

interface ViewToggleProps {
  view: ViewMode;
  onViewChange: (view: ViewMode) => void;
  className?: string;
}

export function ViewToggle({ view, onViewChange, className }: ViewToggleProps) {
  return (
    <div className={cn("flex items-center border rounded-md", className)}>
      <Button
        variant="ghost"
        size="sm"
        className={cn(
          "h-8 px-2 rounded-r-none",
          view === "grid" && "bg-muted"
        )}
        onClick={() => onViewChange("grid")}
        aria-label="Grid view"
        aria-pressed={view === "grid"}
      >
        <LayoutGrid className="h-4 w-4" />
      </Button>
      <Button
        variant="ghost"
        size="sm"
        className={cn(
          "h-8 px-2 rounded-l-none border-l",
          view === "table" && "bg-muted"
        )}
        onClick={() => onViewChange("table")}
        aria-label="Table view"
        aria-pressed={view === "table"}
      >
        <List className="h-4 w-4" />
      </Button>
    </div>
  );
}
