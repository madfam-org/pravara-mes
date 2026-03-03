import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { KanbanCard } from '../card'
import type { Task, Machine } from '@/lib/api'

// Mock DnD Kit sortable
vi.mock('@dnd-kit/sortable', () => ({
  useSortable: () => ({
    attributes: {},
    listeners: {},
    setNodeRef: vi.fn(),
    transform: null,
    transition: null,
    isDragging: false,
  }),
}))

vi.mock('@dnd-kit/utilities', () => ({
  CSS: {
    Transform: {
      toString: () => null,
    },
  },
}))

vi.mock('@/lib/utils', () => ({
  cn: (...args: any[]) => args.filter(Boolean).join(' '),
}))

function makeTask(overrides: Partial<Task> = {}): Task {
  return {
    id: 'task-1',
    tenant_id: 'tenant-1',
    title: 'Print Widget A',
    status: 'backlog',
    priority: 3,
    kanban_position: 1,
    created_at: '2025-01-01T00:00:00Z',
    updated_at: '2025-01-01T00:00:00Z',
    ...overrides,
  }
}

function makeMachine(overrides: Partial<Machine> = {}): Machine {
  return {
    id: 'machine-1',
    tenant_id: 'tenant-1',
    name: 'Ender 3',
    code: 'E3-001',
    type: '3D Printer',
    status: 'online',
    created_at: '2025-01-01T00:00:00Z',
    updated_at: '2025-01-01T00:00:00Z',
    ...overrides,
  }
}

describe('KanbanCard', () => {
  it('renders task title', () => {
    render(<KanbanCard task={makeTask()} />)
    expect(screen.getByText('Print Widget A')).toBeInTheDocument()
  })

  it('renders task description when provided', () => {
    render(<KanbanCard task={makeTask({ description: 'High priority widget' })} />)
    expect(screen.getByText('High priority widget')).toBeInTheDocument()
  })

  it('does not render description when not provided', () => {
    const { container } = render(<KanbanCard task={makeTask({ description: undefined })} />)
    const descriptionParagraphs = container.querySelectorAll('p')
    // Only the title h4 should exist, no description paragraph
    expect(descriptionParagraphs.length).toBe(0)
  })

  it('renders estimated minutes when provided', () => {
    render(<KanbanCard task={makeTask({ estimated_minutes: 45 })} />)
    expect(screen.getByText('45m')).toBeInTheDocument()
  })

  it('renders "Assigned" when assigned_user_id is set', () => {
    render(<KanbanCard task={makeTask({ assigned_user_id: 'user-1' })} />)
    expect(screen.getByText('Assigned')).toBeInTheDocument()
  })

  it('does not render "Assigned" when no user assigned', () => {
    render(<KanbanCard task={makeTask({ assigned_user_id: undefined })} />)
    expect(screen.queryByText('Assigned')).not.toBeInTheDocument()
  })

  it('renders machine name when machine prop is provided', () => {
    render(<KanbanCard task={makeTask()} machine={makeMachine()} />)
    expect(screen.getByText('Ender 3')).toBeInTheDocument()
  })

  it('renders "Machine" fallback when task has machine_id but no machine object', () => {
    render(<KanbanCard task={makeTask({ machine_id: 'machine-1' })} />)
    expect(screen.getByText('Machine')).toBeInTheDocument()
  })

  it('shows warning indicator for machine in error state', () => {
    const { container } = render(
      <KanbanCard
        task={makeTask()}
        machine={makeMachine({ status: 'error' })}
      />
    )
    // Should have ring-1 class for warning
    expect(container.innerHTML).toContain('ring-1')
  })

  it('shows warning indicator for machine in maintenance state', () => {
    const { container } = render(
      <KanbanCard
        task={makeTask()}
        machine={makeMachine({ status: 'maintenance' })}
      />
    )
    expect(container.innerHTML).toContain('ring-1')
  })

  it('calls onClick when card is clicked', () => {
    const onClick = vi.fn()
    render(<KanbanCard task={makeTask()} onClick={onClick} />)
    fireEvent.click(screen.getByText('Print Widget A'))
    expect(onClick).toHaveBeenCalled()
  })

  it('applies different border colors based on priority', () => {
    const { container: p1 } = render(<KanbanCard task={makeTask({ priority: 1 })} />)
    expect(p1.innerHTML).toContain('border-l-red-500')

    const { container: p5 } = render(<KanbanCard task={makeTask({ priority: 5 })} />)
    expect(p5.innerHTML).toContain('border-l-gray-500')
  })

  it('renders command status icon when commandStatus is provided', () => {
    // "completed" command status should render a check icon
    const { container } = render(
      <KanbanCard task={makeTask()} commandStatus="completed" />
    )
    // CheckCircle2 renders an svg
    expect(container.querySelector('svg')).toBeTruthy()
  })
})
