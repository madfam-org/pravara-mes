import type { Machine } from "@/lib/api";

/**
 * Machine counts for the factory floor header, taken from the machines the
 * API returned. Nothing here is estimated: a figure with no source in the
 * data (production totals, efficiency, uptime) is not shown at all.
 */
export interface FactorySummary {
  /** Machines counted below (one page of the list). */
  counted: number;
  /** Running a job right now. */
  running: number;
  /** Reachable and waiting: "online" or "idle". */
  ready: number;
  maintenance: number;
  error: number;
  offline: number;
}

export function summarizeMachines(machines: Machine[]): FactorySummary {
  const summary: FactorySummary = {
    counted: machines.length,
    running: 0,
    ready: 0,
    maintenance: 0,
    error: 0,
    offline: 0,
  };
  for (const machine of machines) {
    switch (machine.status) {
      case "running":
        summary.running += 1;
        break;
      case "online":
      case "idle":
        summary.ready += 1;
        break;
      case "maintenance":
        summary.maintenance += 1;
        break;
      case "error":
        summary.error += 1;
        break;
      default:
        summary.offline += 1;
    }
  }
  return summary;
}

/** Machines a person should look at: in error first, then in maintenance. */
export function machinesNeedingAttention(machines: Machine[]): Machine[] {
  const rank = (m: Machine) => (m.status === "error" ? 0 : 1);
  return machines
    .filter((m) => m.status === "error" || m.status === "maintenance")
    .sort((a, b) => rank(a) - rank(b) || a.name.localeCompare(b.name));
}
