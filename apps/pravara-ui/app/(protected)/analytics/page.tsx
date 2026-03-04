"use client";

import { useState, useMemo } from "react";
import { usePravaraSession } from "@/lib/auth";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import {
  BarChart3,
  RefreshCw,
  TrendingUp,
  Activity,
  CheckCircle,
  Gauge,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  analyticsAPI,
  machinesAPI,
  type OEESnapshot,
  type Machine,
} from "@/lib/api";
import { OEEGauge } from "@/components/analytics/oee-gauge";
import { OEETrendChart } from "@/components/analytics/oee-trend-chart";

function getDefaultDateRange() {
  const to = new Date();
  const from = new Date();
  from.setDate(from.getDate() - 30);
  return {
    from: from.toISOString().split("T")[0],
    to: to.toISOString().split("T")[0],
  };
}

export default function AnalyticsPage() {
  const { data: session } = usePravaraSession();
  const token = (session?.user as any)?.accessToken;
  const queryClient = useQueryClient();

  const defaultRange = getDefaultDateRange();
  const [from, setFrom] = useState(defaultRange.from);
  const [to, setTo] = useState(defaultRange.to);
  const [selectedMachineId, setSelectedMachineId] = useState<string>("");

  // Fetch machines for the filter dropdown
  const { data: machinesData } = useQuery({
    queryKey: ["machines"],
    queryFn: () => machinesAPI.list(token),
    enabled: !!token,
  });
  const machines: Machine[] = machinesData?.data || [];

  // Fetch OEE snapshots
  const oeeParams = useMemo(() => {
    const params = new URLSearchParams();
    if (from) params.set("from", from);
    if (to) params.set("to", to);
    if (selectedMachineId) params.set("machine_id", selectedMachineId);
    return params;
  }, [from, to, selectedMachineId]);

  const { data: oeeData, isLoading } = useQuery({
    queryKey: ["oee", from, to, selectedMachineId],
    queryFn: () => analyticsAPI.getOEE(token, oeeParams),
    enabled: !!token,
  });

  const snapshots: OEESnapshot[] = oeeData?.data || [];

  // Compute OEE mutation
  const computeMutation = useMutation({
    mutationFn: () =>
      analyticsAPI.computeOEE(token, {
        machine_id: selectedMachineId || undefined,
        date: to || undefined,
      }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["oee"] });
    },
  });

  // Fleet-wide averages
  const fleetSummary = useMemo(() => {
    if (snapshots.length === 0) {
      return { availability: 0, performance: 0, quality: 0, oee: 0 };
    }
    const sum = snapshots.reduce(
      (acc, s) => ({
        availability: acc.availability + s.availability,
        performance: acc.performance + s.performance,
        quality: acc.quality + s.quality,
        oee: acc.oee + s.oee,
      }),
      { availability: 0, performance: 0, quality: 0, oee: 0 }
    );
    const count = snapshots.length;
    return {
      availability: sum.availability / count,
      performance: sum.performance / count,
      quality: sum.quality / count,
      oee: sum.oee / count,
    };
  }, [snapshots]);

  // Group snapshots by machine for per-machine gauges
  const perMachineOEE = useMemo(() => {
    const grouped: Record<string, OEESnapshot[]> = {};
    for (const s of snapshots) {
      if (!grouped[s.machine_id]) grouped[s.machine_id] = [];
      grouped[s.machine_id].push(s);
    }
    return Object.entries(grouped).map(([machineId, entries]) => {
      const avg =
        entries.reduce((sum, e) => sum + e.oee, 0) / entries.length;
      const machine = machines.find((m) => m.id === machineId);
      return {
        machineId,
        machineName: machine?.name || machineId.slice(0, 8),
        oee: avg,
        availability:
          entries.reduce((sum, e) => sum + e.availability, 0) / entries.length,
        performance:
          entries.reduce((sum, e) => sum + e.performance, 0) / entries.length,
        quality:
          entries.reduce((sum, e) => sum + e.quality, 0) / entries.length,
      };
    });
  }, [snapshots, machines]);

  // Sort trend data by date
  const trendData = useMemo(
    () =>
      [...snapshots].sort(
        (a, b) =>
          new Date(a.snapshot_date).getTime() -
          new Date(b.snapshot_date).getTime()
      ),
    [snapshots]
  );

  const formatPct = (v: number) => `${(v * 100).toFixed(1)}%`;

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold">Analytics</h1>
          <p className="text-muted-foreground">
            Overall Equipment Effectiveness dashboard
          </p>
        </div>
        <Button
          size="sm"
          onClick={() => computeMutation.mutate()}
          disabled={computeMutation.isPending}
        >
          {computeMutation.isPending ? (
            <RefreshCw className="mr-2 h-4 w-4 animate-spin" />
          ) : (
            <BarChart3 className="mr-2 h-4 w-4" />
          )}
          Compute OEE
        </Button>
      </div>

      {/* Date range + machine filter */}
      <Card>
        <CardContent className="pt-6">
          <div className="flex flex-col gap-4 sm:flex-row sm:items-end">
            <div className="space-y-1.5">
              <Label htmlFor="from-date">From</Label>
              <Input
                id="from-date"
                type="date"
                value={from}
                onChange={(e) => setFrom(e.target.value)}
                className="w-40"
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="to-date">To</Label>
              <Input
                id="to-date"
                type="date"
                value={to}
                onChange={(e) => setTo(e.target.value)}
                className="w-40"
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="machine-filter">Machine</Label>
              <select
                id="machine-filter"
                value={selectedMachineId}
                onChange={(e) => setSelectedMachineId(e.target.value)}
                className="flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring sm:w-52"
              >
                <option value="">All machines</option>
                {machines.map((m) => (
                  <option key={m.id} value={m.id}>
                    {m.name}
                  </option>
                ))}
              </select>
            </div>
          </div>
        </CardContent>
      </Card>

      {/* Fleet OEE Summary Cards */}
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Availability</CardTitle>
            <Activity className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{formatPct(fleetSummary.availability)}</div>
            <p className="text-xs text-muted-foreground">Fleet average uptime</p>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Performance</CardTitle>
            <TrendingUp className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{formatPct(fleetSummary.performance)}</div>
            <p className="text-xs text-muted-foreground">Fleet average speed</p>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Quality</CardTitle>
            <CheckCircle className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{formatPct(fleetSummary.quality)}</div>
            <p className="text-xs text-muted-foreground">Fleet average yield</p>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">OEE</CardTitle>
            <Gauge className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{formatPct(fleetSummary.oee)}</div>
            <p className="text-xs text-muted-foreground">Overall equipment effectiveness</p>
          </CardContent>
        </Card>
      </div>

      {/* Per-machine OEE Gauges */}
      {perMachineOEE.length > 0 && (
        <Card>
          <CardHeader>
            <CardTitle>Machine OEE</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="flex flex-wrap gap-6 justify-center">
              {perMachineOEE.map((m) => (
                <OEEGauge
                  key={m.machineId}
                  value={m.oee}
                  label={m.machineName}
                  size="md"
                />
              ))}
            </div>
          </CardContent>
        </Card>
      )}

      {/* OEE Trend Chart */}
      <Card>
        <CardHeader>
          <CardTitle>OEE Trend</CardTitle>
        </CardHeader>
        <CardContent>
          {isLoading ? (
            <div className="flex items-center justify-center h-[300px]">
              <RefreshCw className="h-6 w-6 animate-spin text-muted-foreground" />
            </div>
          ) : (
            <OEETrendChart data={trendData} height={300} />
          )}
        </CardContent>
      </Card>

      {/* Machine Comparison Table */}
      {perMachineOEE.length > 0 && (
        <Card>
          <CardHeader>
            <CardTitle>Machine Comparison</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b">
                    <th className="text-left py-3 px-2 font-medium">Machine</th>
                    <th className="text-right py-3 px-2 font-medium">OEE</th>
                    <th className="text-right py-3 px-2 font-medium">Availability</th>
                    <th className="text-right py-3 px-2 font-medium">Performance</th>
                    <th className="text-right py-3 px-2 font-medium">Quality</th>
                  </tr>
                </thead>
                <tbody>
                  {perMachineOEE
                    .sort((a, b) => b.oee - a.oee)
                    .map((m) => {
                      const oeeColor =
                        m.oee >= 0.85
                          ? "text-green-600 dark:text-green-400"
                          : m.oee >= 0.6
                            ? "text-yellow-600 dark:text-yellow-400"
                            : "text-red-600 dark:text-red-400";
                      return (
                        <tr key={m.machineId} className="border-b last:border-0">
                          <td className="py-3 px-2 font-medium">{m.machineName}</td>
                          <td className={`py-3 px-2 text-right font-semibold tabular-nums ${oeeColor}`}>
                            {formatPct(m.oee)}
                          </td>
                          <td className="py-3 px-2 text-right tabular-nums">
                            {formatPct(m.availability)}
                          </td>
                          <td className="py-3 px-2 text-right tabular-nums">
                            {formatPct(m.performance)}
                          </td>
                          <td className="py-3 px-2 text-right tabular-nums">
                            {formatPct(m.quality)}
                          </td>
                        </tr>
                      );
                    })}
                </tbody>
              </table>
            </div>
          </CardContent>
        </Card>
      )}

      {/* Empty State */}
      {!isLoading && snapshots.length === 0 && (
        <Card>
          <CardContent className="flex flex-col items-center justify-center py-12">
            <BarChart3 className="h-12 w-12 text-muted-foreground" />
            <h3 className="mt-4 text-lg font-semibold">No OEE data yet</h3>
            <p className="text-muted-foreground text-center max-w-md">
              Click &quot;Compute OEE&quot; to calculate Overall Equipment Effectiveness
              from your machine telemetry and task data.
            </p>
            <Button
              className="mt-4"
              onClick={() => computeMutation.mutate()}
              disabled={computeMutation.isPending}
            >
              <BarChart3 className="mr-2 h-4 w-4" />
              Compute OEE
            </Button>
          </CardContent>
        </Card>
      )}
    </div>
  );
}
