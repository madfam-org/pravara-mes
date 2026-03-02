-- Drop 3D Visualization and Digital Twin Schema

-- Drop triggers
DROP TRIGGER IF EXISTS update_machine_models_updated_at ON machine_models;
DROP TRIGGER IF EXISTS update_factory_layouts_updated_at ON factory_layouts;
DROP TRIGGER IF EXISTS update_cameras_updated_at ON cameras;

-- Drop function
DROP FUNCTION IF EXISTS update_updated_at_column();

-- Drop tables (in reverse dependency order)
DROP TABLE IF EXISTS machine_positions CASCADE;
DROP TABLE IF EXISTS digital_twin_calibrations CASCADE;
DROP TABLE IF EXISTS material_simulations CASCADE;
DROP TABLE IF EXISTS gcode_simulations CASCADE;
DROP TABLE IF EXISTS simulation_cache CASCADE;
DROP TABLE IF EXISTS video_recordings CASCADE;
DROP TABLE IF EXISTS cameras CASCADE;
DROP TABLE IF EXISTS machine_model_associations CASCADE;
DROP TABLE IF EXISTS factory_layouts CASCADE;
DROP TABLE IF EXISTS machine_models CASCADE;