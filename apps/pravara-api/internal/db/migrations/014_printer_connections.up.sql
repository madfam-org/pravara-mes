-- Printer connections and management for 3D printing integration
-- Links discovered machines to active printer connections with profiles and history

-- Printer connection profiles
CREATE TABLE printer_profiles (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name VARCHAR(100) NOT NULL,
    description TEXT,

    -- Printer specifications
    printer_type VARCHAR(50) NOT NULL CHECK (printer_type IN (
        'fdm',          -- Fused Deposition Modeling
        'sla',          -- Stereolithography
        'sls',          -- Selective Laser Sintering
        'dlp',          -- Digital Light Processing
        'binder_jet',   -- Binder Jetting
        'material_jet', -- Material Jetting
        'multi_tool'    -- Multi-tool (3D/Laser/CNC)
    )),
    manufacturer VARCHAR(100),
    model VARCHAR(100),

    -- Build volume
    build_volume_x DECIMAL(10,2), -- mm
    build_volume_y DECIMAL(10,2), -- mm
    build_volume_z DECIMAL(10,2), -- mm

    -- Capabilities
    heated_bed BOOLEAN DEFAULT false,
    heated_chamber BOOLEAN DEFAULT false,
    auto_leveling BOOLEAN DEFAULT false,
    filament_sensor BOOLEAN DEFAULT false,
    power_recovery BOOLEAN DEFAULT false,

    -- Tool capabilities for multi-tool printers
    has_3d_printing BOOLEAN DEFAULT true,
    has_laser BOOLEAN DEFAULT false,
    has_cnc BOOLEAN DEFAULT false,
    has_pen_plotter BOOLEAN DEFAULT false,

    -- Nozzle configurations
    nozzle_diameter DECIMAL(5,2) DEFAULT 0.4, -- mm
    min_nozzle_temp INT DEFAULT 0,
    max_nozzle_temp INT DEFAULT 300,

    -- Bed configurations
    min_bed_temp INT DEFAULT 0,
    max_bed_temp INT DEFAULT 120,

    -- Speed limits
    max_print_speed INT DEFAULT 200, -- mm/s
    max_travel_speed INT DEFAULT 300, -- mm/s
    max_z_speed INT DEFAULT 10, -- mm/s

    -- Default settings
    default_layer_height DECIMAL(5,3) DEFAULT 0.2, -- mm
    default_first_layer_height DECIMAL(5,3) DEFAULT 0.3, -- mm
    default_line_width DECIMAL(5,3) DEFAULT 0.4, -- mm
    default_infill_percent INT DEFAULT 20,

    -- G-code flavor
    gcode_flavor VARCHAR(50) DEFAULT 'marlin' CHECK (gcode_flavor IN (
        'marlin',
        'reprap',
        'klipper',
        'smoothie',
        'grbl',
        'makerbot',
        'flashforge',
        'snapmaker',
        'custom'
    )),

    -- Start/End G-code templates
    start_gcode TEXT,
    end_gcode TEXT,
    layer_change_gcode TEXT,

    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,

    UNIQUE(tenant_id, name)
);

-- Active printer connections
CREATE TABLE printer_connections (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    machine_id UUID REFERENCES machines(id) ON DELETE CASCADE,
    discovered_machine_id UUID REFERENCES discovered_machines(id) ON DELETE SET NULL,
    profile_id UUID REFERENCES printer_profiles(id) ON DELETE SET NULL,

    -- Connection details
    name VARCHAR(100) NOT NULL,
    connection_type VARCHAR(50) NOT NULL CHECK (connection_type IN (
        'serial',
        'network',
        'octoprint',
        'klipper',
        'moonraker',
        'duet',
        'astroprint',
        'repetier',
        'snapmaker_wifi',
        'snapmaker_serial'
    )),

    -- Connection parameters
    connection_url TEXT, -- URL for network connections
    serial_port VARCHAR(100), -- Serial port path
    baud_rate INT DEFAULT 115200,
    api_key TEXT, -- For OctoPrint, Klipper, etc.

    -- Connection state
    is_active BOOLEAN DEFAULT true,
    is_connected BOOLEAN DEFAULT false,
    last_connected_at TIMESTAMP WITH TIME ZONE,
    last_disconnected_at TIMESTAMP WITH TIME ZONE,
    connection_error TEXT,

    -- Current state
    current_state VARCHAR(50) DEFAULT 'idle' CHECK (current_state IN (
        'idle',
        'printing',
        'paused',
        'error',
        'maintenance',
        'offline',
        'connecting',
        'disconnecting'
    )),

    -- Current tool for multi-tool
    current_tool VARCHAR(20) CHECK (current_tool IN (
        '3d_printing',
        'laser',
        'cnc',
        'pen_plotter'
    )),

    -- Temperature readings
    nozzle_temp_current DECIMAL(5,1),
    nozzle_temp_target DECIMAL(5,1),
    bed_temp_current DECIMAL(5,1),
    bed_temp_target DECIMAL(5,1),
    chamber_temp_current DECIMAL(5,1),

    -- Position
    position_x DECIMAL(10,3),
    position_y DECIMAL(10,3),
    position_z DECIMAL(10,3),
    position_e DECIMAL(10,3), -- Extruder position

    -- Current job
    current_job_id UUID, -- References print_jobs(id)

    -- Statistics
    total_print_time_hours DECIMAL(10,2) DEFAULT 0,
    total_filament_used_m DECIMAL(10,2) DEFAULT 0,
    successful_prints INT DEFAULT 0,
    failed_prints INT DEFAULT 0,

    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,

    UNIQUE(tenant_id, name)
);

-- Material profiles
CREATE TABLE material_profiles (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,

    -- Material identification
    name VARCHAR(100) NOT NULL,
    material_type VARCHAR(50) NOT NULL CHECK (material_type IN (
        'PLA',
        'ABS',
        'PETG',
        'TPU',
        'TPE',
        'Nylon',
        'PC',
        'PVA',
        'HIPS',
        'ASA',
        'PP',
        'POM',
        'PEEK',
        'PEI',
        'Resin_Standard',
        'Resin_Tough',
        'Resin_Flexible',
        'Resin_Castable',
        'Resin_Biocompatible',
        'Wood',
        'Metal',
        'Ceramic',
        'Custom'
    )),
    manufacturer VARCHAR(100),
    color VARCHAR(50),

    -- Temperature settings
    nozzle_temp INT,
    bed_temp INT,
    chamber_temp INT,

    -- Print settings
    print_speed INT, -- mm/s
    retract_distance DECIMAL(5,2), -- mm
    retract_speed INT, -- mm/s

    -- Material properties
    density DECIMAL(5,3), -- g/cm³
    diameter DECIMAL(5,3) DEFAULT 1.75, -- mm
    cost_per_kg DECIMAL(10,2),

    -- Environmental settings
    cooling_fan_percent INT DEFAULT 100,
    part_cooling_min_layer INT DEFAULT 2,

    notes TEXT,

    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,

    UNIQUE(tenant_id, name, manufacturer, color)
);

-- Print jobs history
CREATE TABLE print_jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    connection_id UUID REFERENCES printer_connections(id) ON DELETE SET NULL,
    profile_id UUID REFERENCES printer_profiles(id) ON DELETE SET NULL,
    material_id UUID REFERENCES material_profiles(id) ON DELETE SET NULL,

    -- Job identification
    job_name VARCHAR(255) NOT NULL,
    file_name VARCHAR(255),
    file_size_bytes BIGINT,

    -- Source
    source VARCHAR(50) CHECK (source IN (
        'upload',
        'sd_card',
        'usb',
        'network',
        'generated',
        'sliced'
    )),

    -- G-code analysis
    layer_count INT,
    estimated_time_seconds INT,
    estimated_filament_mm DECIMAL(10,2),
    estimated_filament_g DECIMAL(10,2),

    -- Actual metrics
    actual_time_seconds INT,
    actual_filament_mm DECIMAL(10,2),

    -- Job state
    status VARCHAR(50) NOT NULL DEFAULT 'pending' CHECK (status IN (
        'pending',
        'preparing',
        'printing',
        'paused',
        'completed',
        'cancelled',
        'failed'
    )),

    -- Progress
    progress_percent DECIMAL(5,2) DEFAULT 0,
    current_layer INT DEFAULT 0,
    time_elapsed_seconds INT DEFAULT 0,
    time_remaining_seconds INT,

    -- Timestamps
    queued_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    started_at TIMESTAMP WITH TIME ZONE,
    paused_at TIMESTAMP WITH TIME ZONE,
    resumed_at TIMESTAMP WITH TIME ZONE,
    completed_at TIMESTAMP WITH TIME ZONE,
    cancelled_at TIMESTAMP WITH TIME ZONE,
    failed_at TIMESTAMP WITH TIME ZONE,

    -- Error tracking
    error_message TEXT,
    error_code VARCHAR(50),

    -- Multi-tool specific
    tool_type VARCHAR(20) CHECK (tool_type IN (
        '3d_printing',
        'laser',
        'cnc',
        'pen_plotter'
    )),

    -- Laser/CNC specific
    laser_power_percent INT,
    spindle_speed_rpm INT,
    feed_rate_mm_min DECIMAL(10,2),
    pass_count INT DEFAULT 1,
    current_pass INT DEFAULT 0,

    -- Quality metrics
    quality_score DECIMAL(3,2), -- 0.00 to 1.00
    defects_detected INT DEFAULT 0,

    -- References
    order_id UUID REFERENCES orders(id) ON DELETE SET NULL,
    task_id UUID REFERENCES tasks(id) ON DELETE SET NULL,

    -- Metadata
    metadata JSONB DEFAULT '{}'::jsonb,
    thumbnail_url TEXT,

    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Connection event logs
CREATE TABLE printer_connection_logs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    connection_id UUID REFERENCES printer_connections(id) ON DELETE CASCADE,

    event_type VARCHAR(50) NOT NULL CHECK (event_type IN (
        'connected',
        'disconnected',
        'command_sent',
        'command_received',
        'error',
        'warning',
        'state_change',
        'temperature_update',
        'position_update',
        'print_started',
        'print_paused',
        'print_resumed',
        'print_completed',
        'print_cancelled',
        'print_failed',
        'tool_change',
        'filament_change',
        'maintenance'
    )),

    event_data JSONB DEFAULT '{}'::jsonb,
    message TEXT,
    severity VARCHAR(20) CHECK (severity IN ('debug', 'info', 'warning', 'error', 'critical')),

    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Printer maintenance records
CREATE TABLE printer_maintenance (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    connection_id UUID REFERENCES printer_connections(id) ON DELETE CASCADE,

    maintenance_type VARCHAR(50) NOT NULL CHECK (maintenance_type IN (
        'nozzle_change',
        'bed_leveling',
        'belt_tension',
        'lubrication',
        'calibration',
        'firmware_update',
        'cleaning',
        'part_replacement',
        'inspection'
    )),

    description TEXT,
    performed_by VARCHAR(100),

    -- Odometer readings
    hours_at_maintenance DECIMAL(10,2),
    filament_used_at_maintenance DECIMAL(10,2), -- meters

    -- Next maintenance
    next_maintenance_hours DECIMAL(10,2),
    next_maintenance_date DATE,

    performed_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Indexes for performance
CREATE INDEX idx_printer_profiles_tenant ON printer_profiles(tenant_id);
CREATE INDEX idx_printer_connections_tenant ON printer_connections(tenant_id);
CREATE INDEX idx_printer_connections_machine ON printer_connections(machine_id);
CREATE INDEX idx_printer_connections_state ON printer_connections(current_state);
CREATE INDEX idx_material_profiles_tenant ON material_profiles(tenant_id);
CREATE INDEX idx_print_jobs_tenant ON print_jobs(tenant_id);
CREATE INDEX idx_print_jobs_connection ON print_jobs(connection_id);
CREATE INDEX idx_print_jobs_status ON print_jobs(status);
CREATE INDEX idx_print_jobs_started ON print_jobs(started_at);
CREATE INDEX idx_connection_logs_connection ON printer_connection_logs(connection_id);
CREATE INDEX idx_connection_logs_created ON printer_connection_logs(created_at);
CREATE INDEX idx_connection_logs_event ON printer_connection_logs(event_type);
CREATE INDEX idx_maintenance_connection ON printer_maintenance(connection_id);

-- RLS policies
ALTER TABLE printer_profiles ENABLE ROW LEVEL SECURITY;
ALTER TABLE printer_connections ENABLE ROW LEVEL SECURITY;
ALTER TABLE material_profiles ENABLE ROW LEVEL SECURITY;
ALTER TABLE print_jobs ENABLE ROW LEVEL SECURITY;

CREATE POLICY printer_profiles_tenant_isolation ON printer_profiles
    USING (tenant_id = current_setting('app.tenant_id')::uuid);

CREATE POLICY printer_connections_tenant_isolation ON printer_connections
    USING (tenant_id = current_setting('app.tenant_id')::uuid);

CREATE POLICY material_profiles_tenant_isolation ON material_profiles
    USING (tenant_id = current_setting('app.tenant_id')::uuid);

CREATE POLICY print_jobs_tenant_isolation ON print_jobs
    USING (tenant_id = current_setting('app.tenant_id')::uuid);

-- Triggers for updated_at
CREATE TRIGGER printer_profiles_updated_at BEFORE UPDATE ON printer_profiles
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER printer_connections_updated_at BEFORE UPDATE ON printer_connections
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER material_profiles_updated_at BEFORE UPDATE ON material_profiles
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER print_jobs_updated_at BEFORE UPDATE ON print_jobs
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- Foreign key to link print_jobs to printer_connections
ALTER TABLE printer_connections
    ADD CONSTRAINT fk_current_job
    FOREIGN KEY (current_job_id)
    REFERENCES print_jobs(id)
    ON DELETE SET NULL;