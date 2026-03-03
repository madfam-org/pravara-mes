import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { DataTable } from '../data-table'
import type { ColumnDef } from '@tanstack/react-table'

// Mock pagination sub-component
vi.mock('../data-table-pagination', () => ({
  DataTablePagination: () => <div data-testid="pagination" />,
}))

vi.mock('@/lib/utils', () => ({
  cn: (...args: any[]) => args.filter(Boolean).join(' '),
}))

interface TestRow {
  id: string
  name: string
  value: number
}

const columns: ColumnDef<TestRow, any>[] = [
  {
    accessorKey: 'name',
    header: 'Name',
  },
  {
    accessorKey: 'value',
    header: 'Value',
  },
]

const sampleData: TestRow[] = [
  { id: '1', name: 'Alpha', value: 10 },
  { id: '2', name: 'Beta', value: 20 },
  { id: '3', name: 'Gamma', value: 30 },
]

describe('DataTable', () => {
  it('renders column headers', () => {
    render(<DataTable columns={columns} data={sampleData} />)
    expect(screen.getByText('Name')).toBeInTheDocument()
    expect(screen.getByText('Value')).toBeInTheDocument()
  })

  it('renders row data', () => {
    render(<DataTable columns={columns} data={sampleData} />)
    expect(screen.getByText('Alpha')).toBeInTheDocument()
    expect(screen.getByText('Beta')).toBeInTheDocument()
    expect(screen.getByText('Gamma')).toBeInTheDocument()
    expect(screen.getByText('10')).toBeInTheDocument()
  })

  it('renders empty state with default message when no data', () => {
    render(<DataTable columns={columns} data={[]} />)
    expect(screen.getByText('No results.')).toBeInTheDocument()
  })

  it('renders custom empty message', () => {
    render(<DataTable columns={columns} data={[]} emptyMessage="Nothing found" />)
    expect(screen.getByText('Nothing found')).toBeInTheDocument()
  })

  it('renders loading state', () => {
    render(<DataTable columns={columns} data={[]} isLoading={true} />)
    expect(screen.getByText('Loading...')).toBeInTheDocument()
  })

  it('calls onRowClick when a row is clicked', () => {
    const onRowClick = vi.fn()
    render(<DataTable columns={columns} data={sampleData} onRowClick={onRowClick} />)
    fireEvent.click(screen.getByText('Alpha'))
    expect(onRowClick).toHaveBeenCalledWith(sampleData[0])
  })

  it('renders pagination component', () => {
    render(<DataTable columns={columns} data={sampleData} />)
    expect(screen.getByTestId('pagination')).toBeInTheDocument()
  })

  it('applies custom className', () => {
    const { container } = render(
      <DataTable columns={columns} data={sampleData} className="my-table" />
    )
    expect(container.firstChild).toHaveClass('my-table')
  })

  it('renders correct number of rows', () => {
    render(<DataTable columns={columns} data={sampleData} />)
    // 3 data rows + 1 header row = at least 3 visible data cells with "Alpha/Beta/Gamma"
    const rows = screen.getAllByRole('row')
    // 1 header row + 3 data rows
    expect(rows.length).toBe(4)
  })

  it('does not show data rows when loading', () => {
    render(<DataTable columns={columns} data={sampleData} isLoading={true} />)
    expect(screen.queryByText('Alpha')).not.toBeInTheDocument()
  })
})
