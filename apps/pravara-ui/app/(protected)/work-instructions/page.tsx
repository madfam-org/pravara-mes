"use client";

import { useState, useMemo } from "react";
import { usePravaraSession } from "@/lib/auth";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import {
  Plus,
  Search,
  BookOpen,
  ChevronDown,
  ChevronUp,
  Wrench,
  ShieldAlert,
  ListChecks,
  Link as LinkIcon,
  Cpu,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import {
  workInstructionsAPI,
  type WorkInstruction,
  type ListResponse,
} from "@/lib/api";
import { StepList } from "@/components/work-instructions/step-list";

type Category = "setup" | "operation" | "safety" | "maintenance";

const categoryConfig: Record<
  Category,
  { label: string; color: string; icon: React.ElementType }
> = {
  setup: {
    label: "Setup",
    color:
      "bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-400",
    icon: Wrench,
  },
  operation: {
    label: "Operation",
    color:
      "bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-400",
    icon: BookOpen,
  },
  safety: {
    label: "Safety",
    color:
      "bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-400",
    icon: ShieldAlert,
  },
  maintenance: {
    label: "Maintenance",
    color:
      "bg-yellow-100 text-yellow-700 dark:bg-yellow-900/30 dark:text-yellow-400",
    icon: Wrench,
  },
};

export default function WorkInstructionsPage() {
  const { data: session } = usePravaraSession();
  const token = (session?.user as any)?.accessToken;
  const queryClient = useQueryClient();

  const [searchQuery, setSearchQuery] = useState("");
  const [categoryFilter, setCategoryFilter] = useState<Category | null>(null);
  const [expandedId, setExpandedId] = useState<string | null>(null);

  const { data, isLoading } = useQuery({
    queryKey: ["work-instructions"],
    queryFn: () => workInstructionsAPI.list(token),
    enabled: !!token,
  });

  const toggleActiveMutation = useMutation({
    mutationFn: ({
      id,
      is_active,
    }: {
      id: string;
      is_active: boolean;
    }) => workInstructionsAPI.update(token, id, { is_active }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["work-instructions"] });
    },
  });

  const instructions = data?.data || [];

  const filteredInstructions = useMemo(() => {
    return instructions.filter((wi) => {
      if (categoryFilter && wi.category !== categoryFilter) return false;

      if (searchQuery) {
        const query = searchQuery.toLowerCase();
        const matchesSearch =
          wi.title.toLowerCase().includes(query) ||
          wi.description?.toLowerCase().includes(query) ||
          wi.machine_type?.toLowerCase().includes(query);
        if (!matchesSearch) return false;
      }

      return true;
    });
  }, [instructions, categoryFilter, searchQuery]);

  const handleToggleExpand = (id: string) => {
    setExpandedId(expandedId === id ? null : id);
  };

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold">Work Instructions</h1>
          <p className="text-muted-foreground">
            Manage step-by-step instructions for production tasks
          </p>
        </div>
        <Button size="sm">
          <Plus className="mr-2 h-4 w-4" />
          New Instruction
        </Button>
      </div>

      {/* Search */}
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center">
        <div className="relative w-full sm:w-80">
          <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            placeholder="Search instructions..."
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            className="pl-9"
          />
        </div>
      </div>

      {/* Category Filter */}
      <div className="flex flex-wrap gap-2">
        <Button
          variant={categoryFilter === null ? "default" : "outline"}
          size="sm"
          onClick={() => setCategoryFilter(null)}
        >
          All
        </Button>
        {(Object.keys(categoryConfig) as Category[]).map((cat) => {
          const config = categoryConfig[cat];
          const count = instructions.filter(
            (wi) => wi.category === cat
          ).length;
          return (
            <Button
              key={cat}
              variant={categoryFilter === cat ? "default" : "outline"}
              size="sm"
              onClick={() =>
                setCategoryFilter(categoryFilter === cat ? null : cat)
              }
            >
              {config.label}
              <Badge variant="secondary" className="ml-2">
                {count}
              </Badge>
            </Button>
          );
        })}
      </div>

      {/* Instructions List */}
      {isLoading ? (
        <div className="space-y-4">
          {[...Array(4)].map((_, i) => (
            <Card key={i} className="animate-pulse">
              <CardHeader>
                <div className="h-5 w-48 rounded bg-muted" />
              </CardHeader>
              <CardContent>
                <div className="space-y-2">
                  <div className="h-4 w-full rounded bg-muted" />
                  <div className="h-4 w-2/3 rounded bg-muted" />
                </div>
              </CardContent>
            </Card>
          ))}
        </div>
      ) : filteredInstructions.length === 0 ? (
        <Card>
          <CardContent className="flex flex-col items-center justify-center py-12">
            <BookOpen className="h-12 w-12 text-muted-foreground" />
            <h3 className="mt-4 text-lg font-semibold">
              {instructions.length === 0
                ? "No work instructions yet"
                : "No matching instructions"}
            </h3>
            <p className="text-muted-foreground">
              {instructions.length === 0
                ? "Create your first work instruction to get started"
                : "Try adjusting your search or category filter"}
            </p>
            {instructions.length > 0 &&
              (searchQuery || categoryFilter) && (
                <Button
                  variant="outline"
                  className="mt-4"
                  onClick={() => {
                    setSearchQuery("");
                    setCategoryFilter(null);
                  }}
                >
                  Clear filters
                </Button>
              )}
          </CardContent>
        </Card>
      ) : (
        <div className="space-y-4">
          {/* Results count */}
          {(searchQuery || categoryFilter) && (
            <p className="text-sm text-muted-foreground">
              Showing {filteredInstructions.length} of{" "}
              {instructions.length} instructions
            </p>
          )}

          {filteredInstructions.map((wi) => {
            const catConfig = categoryConfig[wi.category];
            const isExpanded = expandedId === wi.id;

            return (
              <Card
                key={wi.id}
                className="hover:shadow-md transition-shadow"
              >
                <CardHeader className="pb-3">
                  <div className="flex items-start justify-between gap-4">
                    <div className="flex-1 min-w-0">
                      <div className="flex items-center gap-2 flex-wrap">
                        <CardTitle className="text-lg">
                          {wi.title}
                        </CardTitle>
                        <Badge variant="outline">{wi.version}</Badge>
                        <Badge className={catConfig.color}>
                          {catConfig.label}
                        </Badge>
                      </div>

                      {wi.description && (
                        <p className="mt-1 text-sm text-muted-foreground line-clamp-2">
                          {wi.description}
                        </p>
                      )}
                    </div>

                    <div className="flex items-center gap-3 shrink-0">
                      {/* Active toggle */}
                      <div className="flex items-center gap-2">
                        <span className="text-xs text-muted-foreground">
                          {wi.is_active ? "Active" : "Inactive"}
                        </span>
                        <Switch
                          checked={wi.is_active}
                          onCheckedChange={(checked) =>
                            toggleActiveMutation.mutate({
                              id: wi.id,
                              is_active: checked,
                            })
                          }
                          aria-label={`Toggle ${wi.title} active status`}
                        />
                      </div>

                      {/* Expand/collapse */}
                      <Button
                        variant="ghost"
                        size="icon"
                        className="h-8 w-8"
                        onClick={() => handleToggleExpand(wi.id)}
                        aria-label={
                          isExpanded ? "Collapse steps" : "Expand steps"
                        }
                      >
                        {isExpanded ? (
                          <ChevronUp className="h-4 w-4" />
                        ) : (
                          <ChevronDown className="h-4 w-4" />
                        )}
                      </Button>
                    </div>
                  </div>
                </CardHeader>

                <CardContent className="pt-0">
                  {/* Metadata row */}
                  <div className="flex flex-wrap items-center gap-3 text-sm">
                    {wi.product_definition_id && (
                      <div className="flex items-center gap-1 text-muted-foreground">
                        <LinkIcon className="h-3.5 w-3.5" />
                        <span>Linked to product</span>
                      </div>
                    )}

                    {wi.machine_type && (
                      <div className="flex items-center gap-1 text-muted-foreground">
                        <Cpu className="h-3.5 w-3.5" />
                        <span>{wi.machine_type}</span>
                      </div>
                    )}

                    <div className="flex items-center gap-1 text-muted-foreground">
                      <ListChecks className="h-3.5 w-3.5" />
                      <span>
                        {wi.steps.length}{" "}
                        {wi.steps.length === 1 ? "step" : "steps"}
                      </span>
                    </div>

                    {/* Tools required */}
                    {wi.tools_required.length > 0 && (
                      <div className="flex items-center gap-1 flex-wrap">
                        {wi.tools_required.map((tool) => (
                          <Badge
                            key={tool}
                            variant="secondary"
                            className="text-xs"
                          >
                            {tool}
                          </Badge>
                        ))}
                      </div>
                    )}

                    {/* PPE required */}
                    {wi.ppe_required.length > 0 && (
                      <div className="flex items-center gap-1 flex-wrap">
                        {wi.ppe_required.map((ppe) => (
                          <Badge
                            key={ppe}
                            variant="warning"
                            className="text-xs"
                          >
                            {ppe}
                          </Badge>
                        ))}
                      </div>
                    )}
                  </div>

                  {/* Expanded step list */}
                  {isExpanded && wi.steps.length > 0 && (
                    <div className="mt-6 border-t pt-4">
                      <StepList steps={wi.steps} readonly />
                    </div>
                  )}
                </CardContent>
              </Card>
            );
          })}
        </div>
      )}
    </div>
  );
}
