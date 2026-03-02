import React from 'react';
import { render, screen, fireEvent } from '@testing-library/react';
import '@testing-library/jest-dom';
import userEvent from '@testing-library/user-event';
import {
  ToolModeSelector,
  PrintingControls,
  LaserControls,
  CNCControls,
} from '../ToolControls';

describe('ToolModeSelector', () => {
  const mockOnToolChange = jest.fn();
  const defaultProps = {
    currentTool: '3d_printing',
    availableTools: ['3d_printing', 'laser', 'cnc', 'pen_plotter'],
    onToolChange: mockOnToolChange,
  };

  beforeEach(() => {
    jest.clearAllMocks();
  });

  it('renders all available tools', () => {
    render(<ToolModeSelector {...defaultProps} />);
    expect(screen.getByText('3D PRINTING')).toBeInTheDocument();
    expect(screen.getByText('LASER')).toBeInTheDocument();
    expect(screen.getByText('CNC')).toBeInTheDocument();
    expect(screen.getByText('PEN PLOTTER')).toBeInTheDocument();
  });

  it('highlights the current tool', () => {
    render(<ToolModeSelector {...defaultProps} />);
    const currentButton = screen.getByText('3D PRINTING').closest('button');
    expect(currentButton).toHaveClass('bg-primary');
  });

  it('calls onToolChange when a tool is selected', async () => {
    render(<ToolModeSelector {...defaultProps} />);
    const laserButton = screen.getByText('LASER').closest('button');

    if (laserButton) {
      await userEvent.click(laserButton);
      expect(mockOnToolChange).toHaveBeenCalledWith('laser');
    }
  });
});

describe('PrintingControls', () => {
  const mockOnSettingsChange = jest.fn();

  beforeEach(() => {
    jest.clearAllMocks();
  });

  it('renders all printing control sections', () => {
    render(<PrintingControls onSettingsChange={mockOnSettingsChange} />);

    expect(screen.getByText(/Nozzle Temperature/)).toBeInTheDocument();
    expect(screen.getByText(/Bed Temperature/)).toBeInTheDocument();
    expect(screen.getByText(/Print Speed/)).toBeInTheDocument();
    expect(screen.getByText(/Layer Height/)).toBeInTheDocument();
    expect(screen.getByText(/Infill Density/)).toBeInTheDocument();
    expect(screen.getByText(/Fan Speed/)).toBeInTheDocument();
  });

  it('displays default temperature values', () => {
    render(<PrintingControls onSettingsChange={mockOnSettingsChange} />);

    expect(screen.getByText(/Nozzle Temperature: 210°C/)).toBeInTheDocument();
    expect(screen.getByText(/Bed Temperature: 60°C/)).toBeInTheDocument();
  });

  it('displays default speed value', () => {
    render(<PrintingControls onSettingsChange={mockOnSettingsChange} />);

    expect(screen.getByText(/Print Speed: 50 mm\/s/)).toBeInTheDocument();
  });

  it('has layer height dropdown with options', async () => {
    render(<PrintingControls onSettingsChange={mockOnSettingsChange} />);

    const layerHeightTrigger = screen.getByRole('combobox');
    await userEvent.click(layerHeightTrigger);

    expect(screen.getByText('0.1 mm (Fine)')).toBeInTheDocument();
    expect(screen.getByText('0.2 mm (Standard)')).toBeInTheDocument();
    expect(screen.getByText('0.3 mm (Draft)')).toBeInTheDocument();
  });
});

describe('LaserControls', () => {
  const mockOnSettingsChange = jest.fn();

  beforeEach(() => {
    jest.clearAllMocks();
  });

  it('renders all laser control sections', () => {
    render(<LaserControls onSettingsChange={mockOnSettingsChange} />);

    expect(screen.getByText(/Laser Power/)).toBeInTheDocument();
    expect(screen.getByText(/Movement Speed/)).toBeInTheDocument();
    expect(screen.getByText(/Number of Passes/)).toBeInTheDocument();
    expect(screen.getByText(/Z Offset/)).toBeInTheDocument();
    expect(screen.getByText('Air Assist')).toBeInTheDocument();
    expect(screen.getByText('Pulsed Mode')).toBeInTheDocument();
  });

  it('displays default laser power', () => {
    render(<LaserControls onSettingsChange={mockOnSettingsChange} />);

    expect(screen.getByText(/Laser Power: 50%/)).toBeInTheDocument();
  });

  it('displays default movement speed', () => {
    render(<LaserControls onSettingsChange={mockOnSettingsChange} />);

    expect(screen.getByText(/Movement Speed: 1000 mm\/min/)).toBeInTheDocument();
  });

  it('shows pulse frequency control when pulsed mode is enabled', async () => {
    render(<LaserControls onSettingsChange={mockOnSettingsChange} />);

    // Initially, pulse frequency should not be visible
    expect(screen.queryByText(/Pulse Frequency/)).not.toBeInTheDocument();

    // Enable pulsed mode
    const pulsedSwitch = screen.getByText('Pulsed Mode').parentElement?.querySelector('button[role="switch"]');
    if (pulsedSwitch) {
      await userEvent.click(pulsedSwitch);
      // Now pulse frequency should be visible
      expect(screen.getByText(/Pulse Frequency: 1000 Hz/)).toBeInTheDocument();
    }
  });
});

describe('CNCControls', () => {
  const mockOnSettingsChange = jest.fn();

  beforeEach(() => {
    jest.clearAllMocks();
  });

  it('renders all CNC control sections', () => {
    render(<CNCControls onSettingsChange={mockOnSettingsChange} />);

    expect(screen.getByText(/Spindle Speed/)).toBeInTheDocument();
    expect(screen.getByText(/Feed Rate/)).toBeInTheDocument();
    expect(screen.getByText(/Plunge Rate/)).toBeInTheDocument();
    expect(screen.getByText(/Depth per Pass/)).toBeInTheDocument();
    expect(screen.getByText(/Total Depth/)).toBeInTheDocument();
    expect(screen.getByText('Tool Type')).toBeInTheDocument();
    expect(screen.getByText(/Tool Diameter/)).toBeInTheDocument();
    expect(screen.getByText('Coolant')).toBeInTheDocument();
    expect(screen.getByText('Climb Milling')).toBeInTheDocument();
  });

  it('displays default spindle speed', () => {
    render(<CNCControls onSettingsChange={mockOnSettingsChange} />);

    expect(screen.getByText(/Spindle Speed: 10000 RPM/)).toBeInTheDocument();
  });

  it('displays default feed rate', () => {
    render(<CNCControls onSettingsChange={mockOnSettingsChange} />);

    expect(screen.getByText(/Feed Rate: 500 mm\/min/)).toBeInTheDocument();
  });

  it('has tool type dropdown with options', async () => {
    render(<CNCControls onSettingsChange={mockOnSettingsChange} />);

    const toolTypeTriggers = screen.getAllByRole('combobox');
    const toolTypeDropdown = toolTypeTriggers[0]; // First dropdown is tool type

    await userEvent.click(toolTypeDropdown);

    expect(screen.getByText('End Mill')).toBeInTheDocument();
    expect(screen.getByText('Ball Nose')).toBeInTheDocument();
    expect(screen.getByText('V-Bit')).toBeInTheDocument();
    expect(screen.getByText('Drill Bit')).toBeInTheDocument();
    expect(screen.getByText('Engraving Bit')).toBeInTheDocument();
  });

  it('has tool diameter dropdown with options', async () => {
    render(<CNCControls onSettingsChange={mockOnSettingsChange} />);

    const toolDiameterTriggers = screen.getAllByRole('combobox');
    const toolDiameterDropdown = toolDiameterTriggers[1]; // Second dropdown is tool diameter

    await userEvent.click(toolDiameterDropdown);

    expect(screen.getByText('1.0 mm')).toBeInTheDocument();
    expect(screen.getByText('2.0 mm')).toBeInTheDocument();
    expect(screen.getByText('3.175 mm (1/8")')).toBeInTheDocument();
    expect(screen.getByText('6.0 mm')).toBeInTheDocument();
    expect(screen.getByText('6.35 mm (1/4")')).toBeInTheDocument();
  });
});