import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import { KanbanBoard } from '../board'
import type { Task, TaskStatus } from '@/lib/api'

// Mock DnD Kit
vi.mock('@dnd-kit/core', () => ({
  DndContext: ({ children }: any) => <div data-testid="dnd-context">{children}</div>,
  DragOverlay: ({ children }: any) => <div data-testid="drag-overlay">{children}</div>,
  PointerSensor: class {},
  useSensor: () => ({}),
  useSensors: () => [],
}))

vi.mock('@dnd-kit/sortable', () => ({
  SortableContext: ({ children }: any) => <div>{children}</div>,
  verticalListSortingStrategy: {},
}))

// Mock child components
vi.mock('../column', () => ({
  KanbanColumn: ({ title, count, children }: any) => (
    <div data-testid={`column-${title}`}>
      <span data-testid={`column-title-${title}`}>{title}</span>
      <span data-testid={`column-count-${title}`}>{count}</span>
      {children}
    </div>
  ),
}))

vi.mock('../card', () => ({
  KanbanCard: ({ task, onClick }: any) => (
    <div data-testid={`card-${task.id}`} onClick={onClick}>
      {task.title}
    </div>
  ),
}))

function makeTask(overrides: Partial<Task> = {}): Task {
  return {
    id: 'task-1',
    tenant_id: 'tenant-1',
    title: 'Test Task',
    status: 'backlog',
    priority: 3,
    kanban_position: 1,
    created_at: '2025-01-01T00:00:00Z',
    updated_at: '2025-01-01T00:00:00Z',
    ...overrides,
  }
}

function makeEmptyBoard(): Record<TaskStatus, Task[]> {
  return {
    backlog: [],
    queued: [],
    in_progress: [],
    quality_check: [],
    completed: [],
    blocked: [],
  }
}

describe('KanbanBoard', () => {
  it('renders all six columns', () => {
    render(
      <KanbanBoard
        tasks={makeEmptyBoard()}
        onTaskMove={vi.fn()}
      />
    )
    expect(screen.getByTestId('column-Backlog')).toBeInTheDocument()
    expect(screen.getByTestId('column-Queued')).toBeInTheDocument()
    expect(screen.getByTestId('column-In Progress')).toBeInTheDocument()
    expect(screen.getByTestId('column-Quality Check')).toBeInTheDocument()
    expect(screen.getByTestId('column-Completed')).toBeInTheDocument()
    expect(screen.getByTestId('column-Blocked')).toBeInTheDocument()
  })

  it('renders tasks in the correct columns', () => {
    const board = makeEmptyBoard()
    board.backlog = [makeTask({ id: 'task-1', title: 'Backlog Task', status: 'backlog' })]
    board.in_progress = [makeTask({ id: 'task-2', title: 'WIP Task', status: 'in_progress' })]

    render(
      <KanbanBoard tasks={board} onTaskMove={vi.fn()} />
    )
    expect(screen.getByText('Backlog Task')).toBeInTheDocument()
    expect(screen.getByText('WIP Task')).toBeInTheDocument()
  })

  it('displays correct task counts per column', () => {
    const board = makeEmptyBoard()
    board.backlog = [
      makeTask({ id: 't1', status: 'backlog' }),
      makeTask({ id: 't2', status: 'backlog' }),
    ]

    render(
      <KanbanBoard tasks={board} onTaskMove={vi.fn()} />
    )
    expect(screen.getByTestId('column-count-Backlog').textContent).toBe('2')
    expect(screen.getByTestId('column-count-Queued').textContent).toBe('0')
  })

  it('calls onTaskClick when a card is clicked', () => {
    const onTaskClick = vi.fn()
    const board = makeEmptyBoard()
    const task = makeTask({ id: 'task-1', title: 'Clickable Task' })
    board.backlog = [task]

    render(
      <KanbanBoard tasks={board} onTaskMove={vi.fn()} onTaskClick={onTaskClick} />
    )
    screen.getByTestId('card-task-1').click()
    expect(onTaskClick).toHaveBeenCalledWith(task)
  })

  it('renders DndContext wrapper', () => {
    render(
      <KanbanBoard tasks={makeEmptyBoard()} onTaskMove={vi.fn()} />
    )
    expect(screen.getByTestId('dnd-context')).toBeInTheDocument()
  })

  it('renders DragOverlay', () => {
    render(
      <KanbanBoard tasks={makeEmptyBoard()} onTaskMove={vi.fn()} />
    )
    expect(screen.getByTestId('drag-overlay')).toBeInTheDocument()
  })

  it('handles empty board without errors', () => {
    const { container } = render(
      <KanbanBoard tasks={makeEmptyBoard()} onTaskMove={vi.fn()} />
    )
    expect(container).toBeTruthy()
  })
})
