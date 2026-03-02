-- 3D Visualization and Digital Twin Schema
-- Supports factory floor visualization, machine models, and physics simulation

-- Machine 3D models library
CREATE TABLE IF NOT EXISTS machine_models (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    machine_type VARCHAR(50) NOT NULL,
    name VARCHAR(255) NOT NULL,
    model_url TEXT NOT NULL, -- URL to GLTF/STL file in S3/R2
    thumbnail_url TEXT,      -- Preview image URL
    bounding_box JSONB NOT NULL DEFAULT '{"min": {"x": 0, "y": 0, "z": 0}, "max": {"x": 0, "y": 0, "z": 0}}',
    origin_offset JSONB NOT NULL DEFAULT '{"x": 0, "y": 0, "z": 0}', -- Calibration offset
    scale FLOAT NOT NULL DEFAULT 1.0,
    lod_levels JSONB DEFAULT '[]', -- Level of detail configurations
    materials JSONB DEFAULT '[]',   -- Material definitions
    animations JSONB DEFAULT '[]',  -- Available animations
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Index for quick lookups by machine type
CREATE INDEX idx_machine_models_type ON machine_models(machine_type);

-- Factory floor layouts
CREATE TABLE IF NOT EXISTS factory_layouts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    floor_plan JSONB NOT NULL DEFAULT '{}', -- 2D floor plan definition
    machine_positions JSONB DEFAULT '[]',    -- Array of machine positions
    camera_presets JSONB DEFAULT '[]',       -- Saved camera views
    lighting_config JSONB DEFAULT '{}',      -- Lighting setup
    grid_settings JSONB DEFAULT '{}',        -- Grid configuration
    zones JSONB DEFAULT '[]',                -- Logical zones (production, storage, etc.)
    waypoints JSONB DEFAULT '[]',            -- Navigation waypoints
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,

    UNIQUE(tenant_id, name)
);

-- RLS for factory layouts
ALTER TABLE factory_layouts ENABLE ROW LEVEL SECURITY;

CREATE POLICY factory_layouts_tenant_isolation ON factory_layouts
    FOR ALL USING (tenant_id = current_setting('app.current_tenant')::UUID);

-- Machine-to-model associations
CREATE TABLE IF NOT EXISTS machine_model_associations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    machine_id UUID NOT NULL REFERENCES machines(id) ON DELETE CASCADE,
    model_id UUID NOT NULL REFERENCES machine_models(id) ON DELETE CASCADE,
    custom_scale FLOAT DEFAULT 1.0,
    custom_offset JSONB DEFAULT '{"x": 0, "y": 0, "z": 0}',
    custom_materials JSONB DEFAULT '{}',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,

    UNIQUE(machine_id)
);

-- Camera configurations for video feeds
CREATE TABLE IF NOT EXISTS cameras (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    stream_url TEXT NOT NULL,
    protocol VARCHAR(20) NOT NULL CHECK (protocol IN ('rtsp', 'http', 'https', 'onvif', 'usb')),
    position JSONB DEFAULT '{"x": 0, "y": 0, "z": 0}',     -- Camera position in 3D space
    orientation JSONB DEFAULT '{"x": 0, "y": 0, "z": 0}',  -- Pitch, yaw, roll
    fov FLOAT DEFAULT 60.0,                                 -- Field of view
    machine_id UUID REFERENCES machines(id) ON DELETE SET NULL, -- Optional machine association
    features JSONB DEFAULT '{}',                            -- PTZ, night vision, etc.
    is_active BOOLEAN DEFAULT true,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- RLS for cameras
ALTER TABLE cameras ENABLE ROW LEVEL SECURITY;

CREATE POLICY cameras_tenant_isolation ON cameras
    FOR ALL USING (tenant_id = current_setting('app.current_tenant')::UUID);

-- Video recordings
CREATE TABLE IF NOT EXISTS video_recordings (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    camera_id UUID NOT NULL REFERENCES cameras(id) ON DELETE CASCADE,
    start_time TIMESTAMP WITH TIME ZONE NOT NULL,
    end_time TIMESTAMP WITH TIME ZONE,
    storage_url TEXT,           -- S3/R2 URL for recorded video
    file_size BIGINT,           -- Size in bytes
    duration_seconds INTEGER,
    events JSONB DEFAULT '[]',  -- Motion events, alerts, anomalies
    metadata JSONB DEFAULT '{}',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Physics simulation cache
CREATE TABLE IF NOT EXISTS simulation_cache (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    simulation_type VARCHAR(50) NOT NULL, -- 'gcode', 'collision', 'material'
    input_hash VARCHAR(64) NOT NULL,      -- SHA256 of input parameters
    input_params JSONB NOT NULL,
    result JSONB NOT NULL,
    computation_time_ms INTEGER,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMP WITH TIME ZONE,

    UNIQUE(simulation_type, input_hash)
);

-- Index for cache expiration cleanup
CREATE INDEX idx_simulation_cache_expires ON simulation_cache(expires_at);

-- G-code simulation results
CREATE TABLE IF NOT EXISTS gcode_simulations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    machine_id UUID REFERENCES machines(id) ON DELETE SET NULL,
    task_id UUID REFERENCES tasks(id) ON DELETE CASCADE,
    gcode_file TEXT NOT NULL,
    tool_path JSONB NOT NULL,           -- Array of path segments
    cycle_time_seconds FLOAT NOT NULL,
    cutting_time_seconds FLOAT,
    rapid_time_seconds FLOAT,
    total_distance_mm FLOAT,
    material_removed_mm3 FLOAT,
    collisions JSONB DEFAULT '[]',
    warnings JSONB DEFAULT '[]',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Material simulation results
CREATE TABLE IF NOT EXISTS material_simulations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    process_type VARCHAR(50) NOT NULL, -- 'milling', 'turning', '3d_printing', 'laser_cutting'
    material_type VARCHAR(50) NOT NULL,
    tool_diameter_mm FLOAT,
    feed_rate_mm_min FLOAT,
    spindle_speed_rpm FLOAT,
    layer_height_mm FLOAT,             -- For 3D printing
    surface_quality FLOAT,              -- 0-1 scale
    tool_wear FLOAT,                   -- 0-1 scale
    max_temperature_c FLOAT,
    cutting_forces JSONB DEFAULT '{}',
    chip_parameters JSONB DEFAULT '{}',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Digital twin calibration
CREATE TABLE IF NOT EXISTS digital_twin_calibrations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    machine_id UUID NOT NULL REFERENCES machines(id) ON DELETE CASCADE,
    calibration_type VARCHAR(50) NOT NULL, -- 'position', 'tool_offset', 'backlash'
    reference_points JSONB NOT NULL,       -- Physical reference points
    transformation_matrix JSONB,           -- 4x4 transformation matrix
    error_metrics JSONB DEFAULT '{}',      -- Calibration accuracy metrics
    performed_by UUID REFERENCES users(id),
    validated_at TIMESTAMP WITH TIME ZONE,
    expires_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,

    UNIQUE(machine_id, calibration_type)
);

-- Real-time position tracking (high-frequency updates)
CREATE TABLE IF NOT EXISTS machine_positions (
    machine_id UUID NOT NULL REFERENCES machines(id) ON DELETE CASCADE,
    timestamp TIMESTAMP WITH TIME ZONE NOT NULL,
    position JSONB NOT NULL,        -- {x, y, z}
    tool_position JSONB,            -- Tool tip position
    rotation JSONB,                 -- {x, y, z} rotation
    feed_rate FLOAT,
    spindle_speed FLOAT,

    PRIMARY KEY (machine_id, timestamp)
) PARTITION BY RANGE (timestamp);

-- Create partitions for position tracking (monthly)
CREATE TABLE machine_positions_2026_03 PARTITION OF machine_positions
    FOR VALUES FROM ('2026-03-01') TO ('2026-04-01');

CREATE TABLE machine_positions_2026_04 PARTITION OF machine_positions
    FOR VALUES FROM ('2026-04-01') TO ('2026-05-01');

-- Index for quick position lookups
CREATE INDEX idx_machine_positions_timestamp ON machine_positions(timestamp DESC);

-- Functions for automatic timestamp updates
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = CURRENT_TIMESTAMP;
    RETURN NEW;
END;
$$ language 'plpgsql';

-- Apply update triggers
CREATE TRIGGER update_machine_models_updated_at BEFORE UPDATE ON machine_models
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_factory_layouts_updated_at BEFORE UPDATE ON factory_layouts
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_cameras_updated_at BEFORE UPDATE ON cameras
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- Insert default machine models
INSERT INTO machine_models (machine_type, name, model_url, bounding_box) VALUES
    ('cnc_3axis', 'Generic 3-Axis CNC', '/models/cnc_3axis_generic.gltf',
     '{"min": {"x": -500, "y": -400, "z": 0}, "max": {"x": 500, "y": 400, "z": 200}}'),
    ('3d_printer_fdm', 'Generic FDM Printer', '/models/3d_printer_fdm_generic.gltf',
     '{"min": {"x": -150, "y": -150, "z": 0}, "max": {"x": 150, "y": 150, "z": 200}}'),
    ('laser_cutter', 'Generic Laser Cutter', '/models/laser_cutter_generic.gltf',
     '{"min": {"x": -300, "y": -200, "z": 0}, "max": {"x": 300, "y": 200, "z": 50}}')
ON CONFLICT DO NOTHING;

-- Comments for documentation
COMMENT ON TABLE machine_models IS 'Library of 3D models for different machine types';
COMMENT ON TABLE factory_layouts IS 'Factory floor layout configurations for 3D visualization';
COMMENT ON TABLE cameras IS 'Video camera configurations for live feeds';
COMMENT ON TABLE simulation_cache IS 'Cache for expensive physics simulations';
COMMENT ON TABLE gcode_simulations IS 'G-code simulation results for cycle time and collision detection';
COMMENT ON TABLE material_simulations IS 'Material processing simulation results';
COMMENT ON TABLE digital_twin_calibrations IS 'Calibration data for physical-digital synchronization';
COMMENT ON TABLE machine_positions IS 'High-frequency position tracking for real-time visualization';