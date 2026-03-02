import React from 'react';
import { render, screen } from '@testing-library/react';
import '@testing-library/jest-dom';
import MultiToolVisualization from '../MultiToolVisualization';

// Mock Three.js and React Three Fiber
jest.mock('@react-three/fiber', () => ({
  Canvas: ({ children }: { children: React.ReactNode }) => <div data-testid="canvas">{children}</div>,
  useFrame: jest.fn(),
  useThree: jest.fn(() => ({ camera: {}, gl: {}, scene: {} })),
}));

jest.mock('@react-three/drei', () => ({
  OrbitControls: () => <div data-testid="orbit-controls" />,
  Grid: () => <div data-testid="grid" />,
  Line: () => <div data-testid="line" />,
  Text: ({ children }: { children: React.ReactNode }) => <div data-testid="text">{children}</div>,
  Box: () => <div data-testid="box" />,
  Cylinder: () => <div data-testid="cylinder" />,
  Cone: () => <div data-testid="cone" />,
  Sphere: () => <div data-testid="sphere" />,
}));

jest.mock('three', () => ({
  Vector3: jest.fn(),
  Group: jest.fn(),
  Mesh: jest.fn(),
}));

describe('MultiToolVisualization', () => {
  const defaultProps = {
    toolType: '3d_printing' as const,
    isActive: false,
    position: { x: 0, y: 0, z: 0 },
  };

  it('renders without crashing', () => {
    render(<MultiToolVisualization {...defaultProps} />);
    expect(screen.getByTestId('canvas')).toBeInTheDocument();
  });

  it('displays the correct tool type in status text', () => {
    render(<MultiToolVisualization {...defaultProps} />);
    expect(screen.getByText(/Tool: 3D PRINTING/)).toBeInTheDocument();
  });

  it('shows active status when isActive is true', () => {
    render(<MultiToolVisualization {...defaultProps} isActive={true} />);
    expect(screen.getByText(/Status: Active/)).toBeInTheDocument();
  });

  it('shows idle status when isActive is false', () => {
    render(<MultiToolVisualization {...defaultProps} isActive={false} />);
    expect(screen.getByText(/Status: Idle/)).toBeInTheDocument();
  });

  it('renders grid when showGrid is true', () => {
    render(<MultiToolVisualization {...defaultProps} showGrid={true} />);
    expect(screen.getByTestId('grid')).toBeInTheDocument();
  });

  it('renders axes labels when showAxes is true', () => {
    render(<MultiToolVisualization {...defaultProps} showAxes={true} />);
    expect(screen.getByText('X')).toBeInTheDocument();
    expect(screen.getByText('Y')).toBeInTheDocument();
    expect(screen.getByText('Z')).toBeInTheDocument();
  });

  it('renders laser tool type correctly', () => {
    render(<MultiToolVisualization {...defaultProps} toolType="laser" />);
    expect(screen.getByText(/Tool: LASER/)).toBeInTheDocument();
  });

  it('renders CNC tool type correctly', () => {
    render(<MultiToolVisualization {...defaultProps} toolType="cnc" />);
    expect(screen.getByText(/Tool: CNC/)).toBeInTheDocument();
  });

  it('renders pen plotter tool type correctly', () => {
    render(<MultiToolVisualization {...defaultProps} toolType="pen_plotter" />);
    expect(screen.getByText(/Tool: PEN PLOTTER/)).toBeInTheDocument();
  });

  it('accepts custom workpiece size', () => {
    render(
      <MultiToolVisualization
        {...defaultProps}
        workpieceSize={[300, 300, 150]}
      />
    );
    expect(screen.getByTestId('canvas')).toBeInTheDocument();
  });

  it('accepts custom camera position', () => {
    render(
      <MultiToolVisualization
        {...defaultProps}
        cameraPosition={[200, 200, 200]}
      />
    );
    expect(screen.getByTestId('canvas')).toBeInTheDocument();
  });

  it('renders tool path when provided', () => {
    const toolPath: [number, number, number][] = [
      [0, 0, 0],
      [10, 10, 0],
      [20, 10, 0],
    ];

    render(
      <MultiToolVisualization
        {...defaultProps}
        toolPath={toolPath}
        isActive={true}
      />
    );
    expect(screen.getByTestId('canvas')).toBeInTheDocument();
  });

  it('accepts custom material type', () => {
    render(
      <MultiToolVisualization
        {...defaultProps}
        material="ABS"
      />
    );
    expect(screen.getByTestId('canvas')).toBeInTheDocument();
  });
});