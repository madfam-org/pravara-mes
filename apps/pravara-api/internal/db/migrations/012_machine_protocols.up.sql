-- Machine protocol definitions and compliance tracking
-- This schema captures the entire universe of digital fabrication machine protocols

-- Protocol standards (ISO 6983, MTConnect, OPC UA, etc.)
CREATE TABLE protocol_standards (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(100) NOT NULL UNIQUE,
    version VARCHAR(50),
    organization VARCHAR(100), -- ISO, ANSI, IEC, etc.
    specification_url TEXT,
    description TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Machine communication protocols
CREATE TABLE machine_protocols (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(100) NOT NULL,
    version VARCHAR(50),
    standard_id UUID REFERENCES protocol_standards(id),
    protocol_type VARCHAR(50) NOT NULL CHECK (protocol_type IN (
        'gcode',        -- G-code based
        'proprietary',  -- Vendor-specific
        'api',          -- REST/HTTP API
        'binary',       -- Binary protocol
        'text'          -- Text-based protocol
    )),
    communication_methods JSONB DEFAULT '[]'::jsonb, -- ["serial", "tcp", "udp", "websocket", "mqtt"]
    default_settings JSONB DEFAULT '{}'::jsonb, -- {"baud_rate": 115200, "port": 5000}
    documentation_url TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(name, version)
);

-- Machine firmware catalog
CREATE TABLE machine_firmwares (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    manufacturer VARCHAR(100),
    product_line VARCHAR(100),
    model VARCHAR(100),
    firmware_name VARCHAR(100) NOT NULL,
    firmware_version VARCHAR(50),
    firmware_type VARCHAR(50) NOT NULL CHECK (firmware_type IN (
        'grbl',
        'grblhal',
        'marlin',
        'klipper',
        'reprapfirmware',
        'linuxcnc',
        'smoothieware',
        'fluidnc',
        'fanuc',
        'siemens',
        'haas',
        'mazak',
        'heidenhain',
        'ruida',
        'trocen',
        'epilog',
        'trotec',
        'bambu',
        'custom'
    )),
    protocol_id UUID REFERENCES machine_protocols(id),
    machine_type VARCHAR(50) NOT NULL CHECK (machine_type IN (
        'cnc_3axis',
        'cnc_4axis',
        'cnc_5axis',
        'cnc_router',
        '3d_printer_fdm',
        '3d_printer_sla',
        '3d_printer_sls',
        'laser_co2',
        'laser_fiber',
        'laser_diode',
        'waterjet',
        'plasma_cutter',
        'vinyl_cutter',
        'embroidery',
        'pick_place',
        'robot_arm'
    )),
    capabilities JSONB DEFAULT '{}'::jsonb, -- {"axes": 3, "spindle": true, "coolant": true}
    specifications JSONB DEFAULT '{}'::jsonb, -- {"max_feedrate": 5000, "work_area": {"x": 300, "y": 300, "z": 100}}
    market_share_percent DECIMAL(5,2), -- Estimated market share percentage
    release_date DATE,
    end_of_life_date DATE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Adapter compatibility matrix
CREATE TABLE adapter_compatibility (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    adapter_name VARCHAR(100) NOT NULL,
    adapter_version VARCHAR(50),
    firmware_id UUID REFERENCES machine_firmwares(id),
    protocol_id UUID REFERENCES machine_protocols(id),
    compliance_level VARCHAR(20) NOT NULL CHECK (compliance_level IN (
        'full',         -- 100% protocol implementation
        'core',         -- 80%+ essential features
        'basic',        -- 60%+ minimum viable
        'experimental', -- In development/testing
        'planned'       -- On roadmap
    )),
    compliance_percentage INT CHECK (compliance_percentage >= 0 AND compliance_percentage <= 100),
    supported_features JSONB DEFAULT '[]'::jsonb, -- ["motion", "spindle", "coolant", "probing"]
    unsupported_features JSONB DEFAULT '[]'::jsonb,
    known_issues TEXT,
    test_status VARCHAR(20) CHECK (test_status IN (
        'passed',
        'failed',
        'partial',
        'untested'
    )),
    last_tested_at TIMESTAMP WITH TIME ZONE,
    notes TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(adapter_name, adapter_version, firmware_id)
);

-- Protocol compliance tracking
CREATE TABLE protocol_compliance (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    adapter_name VARCHAR(100) NOT NULL,
    standard_id UUID REFERENCES protocol_standards(id),

    -- G-code compliance (ISO 6983)
    gcode_motion BOOLEAN DEFAULT false,      -- G0, G1
    gcode_arc BOOLEAN DEFAULT false,         -- G2, G3
    gcode_planes BOOLEAN DEFAULT false,      -- G17, G18, G19
    gcode_units BOOLEAN DEFAULT false,       -- G20, G21
    gcode_coordinates BOOLEAN DEFAULT false,  -- G54-G59
    gcode_modes BOOLEAN DEFAULT false,       -- G90, G91
    gcode_canned_cycles BOOLEAN DEFAULT false, -- G81-G89

    -- MTConnect compliance
    mtconnect_assets BOOLEAN DEFAULT false,
    mtconnect_dataitems BOOLEAN DEFAULT false,
    mtconnect_streams BOOLEAN DEFAULT false,
    mtconnect_events BOOLEAN DEFAULT false,
    mtconnect_samples BOOLEAN DEFAULT false,
    mtconnect_conditions BOOLEAN DEFAULT false,

    -- Safety compliance
    safety_estop BOOLEAN DEFAULT false,
    safety_interlock BOOLEAN DEFAULT false,
    safety_limits BOOLEAN DEFAULT false,
    safety_sil_level INT, -- Safety Integrity Level (0-4)

    compliance_notes TEXT,
    certification_status VARCHAR(50),
    certification_date DATE,
    certified_by VARCHAR(100),

    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(adapter_name, standard_id)
);

-- Machine discovery results
CREATE TABLE discovered_machines (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    machine_id UUID REFERENCES machines(id) ON DELETE SET NULL,

    -- Discovery details
    discovery_method VARCHAR(50) CHECK (discovery_method IN (
        'mdns',        -- mDNS/Bonjour
        'ssdp',        -- SSDP/UPnP
        'usb',         -- USB enumeration
        'network_scan', -- TCP/UDP scan
        'bluetooth',   -- BLE discovery
        'manual'       -- Manual entry
    )),
    discovered_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,

    -- Machine information
    hostname VARCHAR(255),
    ip_address INET,
    mac_address MACADDR,
    port INT,
    serial_port VARCHAR(100), -- /dev/ttyUSB0, COM3, etc.

    -- Identification
    manufacturer VARCHAR(100),
    model VARCHAR(100),
    serial_number VARCHAR(100),
    firmware_version VARCHAR(50),
    detected_firmware_id UUID REFERENCES machine_firmwares(id),

    -- Connection details
    connection_type VARCHAR(50), -- serial, tcp, udp, websocket
    connection_parameters JSONB DEFAULT '{}'::jsonb,

    -- Status
    status VARCHAR(20) DEFAULT 'discovered' CHECK (status IN (
        'discovered',
        'connecting',
        'connected',
        'registered',
        'failed',
        'ignored'
    )),
    last_seen_at TIMESTAMP WITH TIME ZONE,
    registration_attempted_at TIMESTAMP WITH TIME ZONE,
    error_message TEXT,

    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Performance metrics for protocol adapters
CREATE TABLE adapter_metrics (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    adapter_name VARCHAR(100) NOT NULL,
    firmware_id UUID REFERENCES machine_firmwares(id),

    -- Performance metrics
    command_latency_ms DECIMAL(10,2), -- Average command latency
    status_update_rate_hz DECIMAL(10,2), -- Status updates per second
    telemetry_rate_hz DECIMAL(10,2), -- Telemetry updates per second

    -- Reliability metrics
    uptime_percent DECIMAL(5,2), -- Uptime percentage
    commands_sent BIGINT DEFAULT 0,
    commands_succeeded BIGINT DEFAULT 0,
    commands_failed BIGINT DEFAULT 0,
    connection_failures INT DEFAULT 0,

    -- Throughput metrics
    max_commands_per_second INT,
    avg_commands_per_second DECIMAL(10,2),
    data_transferred_bytes BIGINT DEFAULT 0,

    measurement_period_start TIMESTAMP WITH TIME ZONE,
    measurement_period_end TIMESTAMP WITH TIME ZONE,

    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Indexes for performance
CREATE INDEX idx_machine_protocols_standard ON machine_protocols(standard_id);
CREATE INDEX idx_machine_firmwares_protocol ON machine_firmwares(protocol_id);
CREATE INDEX idx_machine_firmwares_type ON machine_firmwares(machine_type);
CREATE INDEX idx_adapter_compatibility_adapter ON adapter_compatibility(adapter_name);
CREATE INDEX idx_adapter_compatibility_firmware ON adapter_compatibility(firmware_id);
CREATE INDEX idx_discovered_machines_tenant ON discovered_machines(tenant_id);
CREATE INDEX idx_discovered_machines_status ON discovered_machines(status);
CREATE INDEX idx_adapter_metrics_adapter ON adapter_metrics(adapter_name);

-- RLS policies
ALTER TABLE discovered_machines ENABLE ROW LEVEL SECURITY;

CREATE POLICY discovered_machines_tenant_isolation ON discovered_machines
    USING (tenant_id = current_setting('app.tenant_id')::uuid);

-- Initial seed data for standards
INSERT INTO protocol_standards (name, version, organization, specification_url, description) VALUES
('ISO 6983', '2009', 'ISO', 'https://www.iso.org/standard/34608.html', 'Numerical control - Program format and definition of address words'),
('MTConnect', '1.7', 'MTConnect Institute', 'https://www.mtconnect.org/standard', 'Manufacturing Technology Connect standard'),
('OPC UA', 'IEC 62541', 'OPC Foundation', 'https://opcfoundation.org/developer-tools/specifications-unified-architecture', 'Open Platform Communications Unified Architecture'),
('MQTT', 'v5.0', 'OASIS', 'https://docs.oasis-open.org/mqtt/mqtt/v5.0/mqtt-v5.0.html', 'Message Queuing Telemetry Transport'),
('Modbus', 'V1.1b3', 'Modbus Organization', 'https://modbus.org/specs.php', 'Modbus Application Protocol Specification');

-- Triggers for updated_at
CREATE TRIGGER protocol_standards_updated_at BEFORE UPDATE ON protocol_standards
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();

CREATE TRIGGER machine_protocols_updated_at BEFORE UPDATE ON machine_protocols
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();

CREATE TRIGGER machine_firmwares_updated_at BEFORE UPDATE ON machine_firmwares
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();

CREATE TRIGGER adapter_compatibility_updated_at BEFORE UPDATE ON adapter_compatibility
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();

CREATE TRIGGER protocol_compliance_updated_at BEFORE UPDATE ON protocol_compliance
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();

CREATE TRIGGER discovered_machines_updated_at BEFORE UPDATE ON discovered_machines
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();