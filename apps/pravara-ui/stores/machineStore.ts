import { create } from 'zustand';
import { devtools, persist } from 'zustand/middleware';

export interface Machine {
  id: string;
  name: string;
  code: string;
  type: string;
  status: 'online' | 'offline' | 'error' | 'maintenance';
  location?: string;
  specifications?: Record<string, any>;
  metadata?: Record<string, any>;
  lastHeartbeat?: string;
}

interface MachineState {
  machines: Record<string, Machine>;
  selectedMachineId: string | null;
  addMachine: (machine: Machine) => void;
  updateMachine: (id: string, updates: Partial<Machine>) => void;
  deleteMachine: (id: string) => void;
  selectMachine: (id: string | null) => void;
  setMachines: (machines: Machine[]) => void;
  getMachine: (id: string) => Machine | undefined;
}

export const useMachineStore = create<MachineState>()(
  devtools(
    persist(
      (set, get) => ({
        machines: {},
        selectedMachineId: null,

        addMachine: (machine) =>
          set((state) => ({
            machines: {
              ...state.machines,
              [machine.id]: machine,
            },
          })),

        updateMachine: (id, updates) =>
          set((state) => ({
            machines: {
              ...state.machines,
              [id]: {
                ...state.machines[id],
                ...updates,
              },
            },
          })),

        deleteMachine: (id) =>
          set((state) => {
            const { [id]: deleted, ...rest } = state.machines;
            return { machines: rest };
          }),

        selectMachine: (id) =>
          set(() => ({
            selectedMachineId: id,
          })),

        setMachines: (machines) =>
          set(() => ({
            machines: machines.reduce((acc, machine) => {
              acc[machine.id] = machine;
              return acc;
            }, {} as Record<string, Machine>),
          })),

        getMachine: (id) => get().machines[id],
      }),
      {
        name: 'machine-store',
      }
    )
  )
);