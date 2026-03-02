"use client";

import * as React from "react";
import { Filter, X, Check } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Separator } from "@/components/ui/separator";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { cn } from "@/lib/utils";

export interface FilterOption {
  value: string;
  label: string;
  count?: number;
}

export interface FilterGroup {
  id: string;
  label: string;
  options: FilterOption[];
  multiple?: boolean;
}

export interface FilterState {
  [groupId: string]: string[];
}

interface FilterPopoverProps {
  groups: FilterGroup[];
  value?: FilterState;
  onChange?: (value: FilterState) => void;
  className?: string;
}

export function FilterPopover({
  groups,
  value = {},
  onChange,
  className,
}: FilterPopoverProps) {
  const [open, setOpen] = React.useState(false);

  const activeFilterCount = React.useMemo(() => {
    return Object.values(value).reduce(
      (acc, filters) => acc + filters.length,
      0
    );
  }, [value]);

  const handleOptionToggle = React.useCallback(
    (groupId: string, optionValue: string, multiple: boolean = true) => {
      const currentValues = value[groupId] || [];
      let newValues: string[];

      if (multiple) {
        if (currentValues.includes(optionValue)) {
          newValues = currentValues.filter((v) => v !== optionValue);
        } else {
          newValues = [...currentValues, optionValue];
        }
      } else {
        // Single select - toggle off if same value
        newValues = currentValues.includes(optionValue) ? [] : [optionValue];
      }

      const newState = {
        ...value,
        [groupId]: newValues,
      };

      // Clean up empty arrays
      if (newValues.length === 0) {
        delete newState[groupId];
      }

      onChange?.(newState);
    },
    [value, onChange]
  );

  const handleClearAll = React.useCallback(() => {
    onChange?.({});
  }, [onChange]);

  const handleClearGroup = React.useCallback(
    (groupId: string) => {
      const newState = { ...value };
      delete newState[groupId];
      onChange?.(newState);
    },
    [value, onChange]
  );

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          variant="outline"
          size="sm"
          className={cn("gap-2", className)}
          aria-label={`Filters${activeFilterCount > 0 ? `, ${activeFilterCount} active` : ""}`}
        >
          <Filter className="h-4 w-4" />
          Filter
          {activeFilterCount > 0 && (
            <Badge
              variant="secondary"
              className="ml-1 h-5 min-w-5 rounded-full px-1.5"
            >
              {activeFilterCount}
            </Badge>
          )}
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-72 p-0" align="start">
        <div className="flex items-center justify-between px-4 py-3">
          <h4 className="text-sm font-medium">Filters</h4>
          {activeFilterCount > 0 && (
            <Button
              variant="ghost"
              size="sm"
              className="h-auto px-2 py-1 text-xs"
              onClick={handleClearAll}
            >
              Clear all
            </Button>
          )}
        </div>
        <Separator />
        <div className="max-h-80 overflow-y-auto">
          {groups.map((group, groupIndex) => {
            const selectedValues = value[group.id] || [];

            return (
              <div key={group.id}>
                {groupIndex > 0 && <Separator />}
                <div className="p-4 space-y-2">
                  <div className="flex items-center justify-between">
                    <span className="text-sm font-medium text-muted-foreground">
                      {group.label}
                    </span>
                    {selectedValues.length > 0 && (
                      <Button
                        variant="ghost"
                        size="sm"
                        className="h-auto px-1 py-0 text-xs"
                        onClick={() => handleClearGroup(group.id)}
                      >
                        <X className="h-3 w-3" />
                      </Button>
                    )}
                  </div>
                  <div className="flex flex-wrap gap-2">
                    {group.options.map((option) => {
                      const isSelected = selectedValues.includes(option.value);

                      return (
                        <button
                          key={option.value}
                          type="button"
                          onClick={() =>
                            handleOptionToggle(
                              group.id,
                              option.value,
                              group.multiple !== false
                            )
                          }
                          className={cn(
                            "inline-flex items-center gap-1.5 rounded-md border px-2.5 py-1 text-xs font-medium transition-colors",
                            isSelected
                              ? "border-primary bg-primary/10 text-primary"
                              : "border-input bg-background hover:bg-accent hover:text-accent-foreground"
                          )}
                          aria-pressed={isSelected}
                        >
                          {isSelected && <Check className="h-3 w-3" />}
                          {option.label}
                          {option.count !== undefined && (
                            <span className="text-muted-foreground">
                              ({option.count})
                            </span>
                          )}
                        </button>
                      );
                    })}
                  </div>
                </div>
              </div>
            );
          })}
        </div>
      </PopoverContent>
    </Popover>
  );
}

// Active filter badges display component
interface ActiveFiltersProps {
  groups: FilterGroup[];
  value: FilterState;
  onChange: (value: FilterState) => void;
  className?: string;
}

export function ActiveFilters({
  groups,
  value,
  onChange,
  className,
}: ActiveFiltersProps) {
  const activeFilters = React.useMemo(() => {
    const filters: { groupId: string; groupLabel: string; value: string; label: string }[] = [];

    for (const group of groups) {
      const selectedValues = value[group.id] || [];
      for (const optionValue of selectedValues) {
        const option = group.options.find((o) => o.value === optionValue);
        if (option) {
          filters.push({
            groupId: group.id,
            groupLabel: group.label,
            value: option.value,
            label: option.label,
          });
        }
      }
    }

    return filters;
  }, [groups, value]);

  const handleRemove = React.useCallback(
    (groupId: string, optionValue: string) => {
      const currentValues = value[groupId] || [];
      const newValues = currentValues.filter((v) => v !== optionValue);
      const newState = { ...value };

      if (newValues.length === 0) {
        delete newState[groupId];
      } else {
        newState[groupId] = newValues;
      }

      onChange(newState);
    },
    [value, onChange]
  );

  const handleClearAll = React.useCallback(() => {
    onChange({});
  }, [onChange]);

  if (activeFilters.length === 0) {
    return null;
  }

  return (
    <div className={cn("flex flex-wrap items-center gap-2", className)}>
      {activeFilters.map((filter) => (
        <Badge
          key={`${filter.groupId}-${filter.value}`}
          variant="secondary"
          className="gap-1 pr-1"
        >
          <span className="text-muted-foreground">{filter.groupLabel}:</span>
          {filter.label}
          <Button
            variant="ghost"
            size="icon"
            className="h-4 w-4 hover:bg-transparent"
            onClick={() => handleRemove(filter.groupId, filter.value)}
            aria-label={`Remove ${filter.label} filter`}
          >
            <X className="h-3 w-3" />
          </Button>
        </Badge>
      ))}
      <Button
        variant="ghost"
        size="sm"
        className="h-6 px-2 text-xs"
        onClick={handleClearAll}
      >
        Clear all
      </Button>
    </div>
  );
}
