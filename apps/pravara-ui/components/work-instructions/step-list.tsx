"use client";

import { AlertTriangle, CheckCircle2, Clock, ImageIcon } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Checkbox } from "@/components/ui/checkbox";
import type { WorkInstructionStep } from "@/lib/api";

interface StepListProps {
  steps: WorkInstructionStep[];
  acknowledgements?: Record<string, { acknowledged_at: string }>;
  onAcknowledge?: (stepNumber: number) => void;
  readonly?: boolean;
}

export function StepList({
  steps,
  acknowledgements,
  onAcknowledge,
  readonly,
}: StepListProps) {
  const sortedSteps = [...steps].sort(
    (a, b) => a.step_number - b.step_number
  );

  return (
    <div className="relative" role="list" aria-label="Work instruction steps">
      {sortedSteps.map((step, index) => {
        const isLast = index === sortedSteps.length - 1;
        const stepKey = String(step.step_number);
        const isAcknowledged = !!acknowledgements?.[stepKey];

        return (
          <div
            key={step.step_number}
            className="relative flex gap-4"
            role="listitem"
          >
            {/* Vertical connector line */}
            {!isLast && (
              <div
                className="absolute left-5 top-10 bottom-0 w-px bg-border"
                aria-hidden="true"
              />
            )}

            {/* Step number circle */}
            <div className="relative z-10 flex shrink-0">
              <div
                className={`flex h-10 w-10 items-center justify-center rounded-full border-2 text-sm font-bold ${
                  isAcknowledged
                    ? "border-green-500 bg-green-50 text-green-700 dark:bg-green-900/30 dark:text-green-400 dark:border-green-600"
                    : "border-border bg-background text-foreground"
                }`}
              >
                {isAcknowledged ? (
                  <CheckCircle2 className="h-5 w-5 text-green-600 dark:text-green-400" />
                ) : (
                  step.step_number
                )}
              </div>
            </div>

            {/* Step content */}
            <div className={`flex-1 pb-8 ${isLast ? "pb-0" : ""}`}>
              <div className="rounded-lg border bg-card p-4">
                <div className="flex items-start justify-between gap-3">
                  <div className="flex-1 min-w-0">
                    <h4 className="font-semibold text-sm">
                      {step.title}
                    </h4>

                    {step.description && (
                      <p className="mt-1 text-sm text-muted-foreground whitespace-pre-wrap">
                        {step.description}
                      </p>
                    )}
                  </div>

                  {/* Duration estimate */}
                  {step.duration_minutes != null && (
                    <div className="flex items-center gap-1 text-xs text-muted-foreground shrink-0">
                      <Clock className="h-3.5 w-3.5" />
                      <span>{step.duration_minutes} min</span>
                    </div>
                  )}
                </div>

                {/* Media thumbnail placeholder */}
                {step.media_url && (
                  <div className="mt-3 flex items-center justify-center rounded-md border border-dashed bg-muted/50 h-32 w-full max-w-xs">
                    <div className="flex flex-col items-center gap-1 text-muted-foreground">
                      <ImageIcon className="h-8 w-8" />
                      <span className="text-xs">Media attachment</span>
                    </div>
                  </div>
                )}

                {/* Warning */}
                {step.warning && (
                  <div className="mt-3">
                    <Badge
                      variant="warning"
                      className="inline-flex items-center gap-1 px-2.5 py-1"
                    >
                      <AlertTriangle className="h-3.5 w-3.5" />
                      {step.warning}
                    </Badge>
                  </div>
                )}

                {/* Acknowledgement */}
                {onAcknowledge && !readonly && (
                  <div className="mt-3 flex items-center gap-2 border-t pt-3">
                    <Checkbox
                      id={`ack-step-${step.step_number}`}
                      checked={isAcknowledged}
                      onCheckedChange={() =>
                        onAcknowledge(step.step_number)
                      }
                      disabled={isAcknowledged}
                      aria-label={`Acknowledge step ${step.step_number}: ${step.title}`}
                    />
                    <label
                      htmlFor={`ack-step-${step.step_number}`}
                      className="text-sm text-muted-foreground cursor-pointer select-none"
                    >
                      {isAcknowledged
                        ? `Acknowledged${
                            acknowledgements?.[stepKey]?.acknowledged_at
                              ? ` at ${new Date(acknowledgements[stepKey].acknowledged_at).toLocaleString()}`
                              : ""
                          }`
                        : "I have completed this step"}
                    </label>
                  </div>
                )}

                {/* Read-only acknowledged indicator */}
                {readonly && isAcknowledged && (
                  <div className="mt-3 flex items-center gap-1.5 border-t pt-3 text-xs text-green-600 dark:text-green-400">
                    <CheckCircle2 className="h-3.5 w-3.5" />
                    <span>
                      Acknowledged
                      {acknowledgements?.[stepKey]?.acknowledged_at
                        ? ` at ${new Date(acknowledgements[stepKey].acknowledged_at).toLocaleString()}`
                        : ""}
                    </span>
                  </div>
                )}
              </div>
            </div>
          </div>
        );
      })}
    </div>
  );
}
