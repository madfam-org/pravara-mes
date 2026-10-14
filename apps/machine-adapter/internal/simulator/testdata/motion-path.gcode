; pravara-mes simulator fixture: motion telemetry path (simulator only, no machine).
; A 60 mm square at Z 0.3 inside a 350 x 350 travel, with extrusion and a retract.
; Durations at the programmed feed rates (no acceleration in the simulator):
; 0.5 s + 1.06 s + 0.47 s + 4 x 0.6 s + 0.02 s + 0.47 s = about 4.9 s.
G28                      ; home all axes (the simulator marks them homed)
G90                      ; absolute XYZ
M83                      ; relative E
G1 Z5 F600               ; lift
G1 X150 Y150 F12000      ; travel
G1 Z0.3 F600             ; first layer height
G1 X210 Y150 E2.0 F6000  ; square, side 1
G1 X210 Y210 E2.0        ; side 2
G1 X150 Y210 E2.0        ; side 3
G1 X150 Y150 E2.0        ; side 4
G1 E-0.8 F2400           ; retract
G1 Z5 F600               ; lift
M400                     ; not modelled (listed in Skipped)
