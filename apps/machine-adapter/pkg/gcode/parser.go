// Package gcode provides G-code generation and parsing utilities.
package gcode

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Command represents a parsed G-code command.
type Command struct {
	Letter     string             // Command letter (G, M, T, etc.)
	Number     int                // Command number
	Parameters map[string]float64 // Parameters (X, Y, Z, F, S, etc.)
	Comment    string             // Inline comment
	Raw        string             // Original line
}

// Parser parses G-code commands.
type Parser struct {
	reader *bufio.Reader
}

// NewParser creates a new G-code parser.
func NewParser(r io.Reader) *Parser {
	return &Parser{
		reader: bufio.NewReader(r),
	}
}

// ParseLine parses a single line of G-code.
func ParseLine(line string) (*Command, error) {
	cmd := &Command{
		Parameters: make(map[string]float64),
		Raw:        line,
	}

	// Remove comments
	if idx := strings.Index(line, ";"); idx >= 0 {
		cmd.Comment = strings.TrimSpace(line[idx+1:])
		line = line[:idx]
	}
	if idx := strings.Index(line, "("); idx >= 0 {
		if endIdx := strings.Index(line, ")"); endIdx > idx {
			cmd.Comment = strings.TrimSpace(line[idx+1 : endIdx])
			line = line[:idx] + line[endIdx+1:]
		}
	}

	line = strings.TrimSpace(line)
	if line == "" {
		return cmd, nil
	}

	// Parse command and parameters
	parts := strings.Fields(line)
	for _, part := range parts {
		if len(part) < 2 {
			continue
		}

		letter := string(part[0])
		valueStr := part[1:]

		// Handle command letters
		if letter == "G" || letter == "M" || letter == "T" {
			if num, err := strconv.Atoi(valueStr); err == nil {
				if cmd.Letter == "" {
					cmd.Letter = letter
					cmd.Number = num
				}
			}
		} else {
			// Handle parameters
			if val, err := strconv.ParseFloat(valueStr, 64); err == nil {
				cmd.Parameters[letter] = val
			}
		}
	}

	return cmd, nil
}

// Next reads and parses the next command.
func (p *Parser) Next() (*Command, error) {
	for {
		line, err := p.reader.ReadString('\n')
		if err != nil {
			if err == io.EOF && line != "" {
				return ParseLine(line)
			}
			return nil, err
		}

		cmd, err := ParseLine(line)
		if err != nil {
			return nil, err
		}

		// Skip empty lines
		if cmd.Letter != "" || len(cmd.Parameters) > 0 {
			return cmd, nil
		}
	}
}

// Generator generates G-code commands.
type Generator struct {
	writer io.Writer
	modal  ModalState
}

// ModalState tracks modal G-code states.
type ModalState struct {
	MotionMode     int     // G0, G1, G2, G3
	FeedRate       float64 // F
	SpindleSpeed   float64 // S
	CoordinateMode int     // G90/G91 (absolute/relative)
	Units          int     // G20/G21 (inch/mm)
	WorkCoordinate int     // G54-G59
}

// NewGenerator creates a new G-code generator.
func NewGenerator(w io.Writer) *Generator {
	return &Generator{
		writer: w,
		modal: ModalState{
			MotionMode:     0,  // G0 rapid
			CoordinateMode: 90, // G90 absolute
			Units:          21, // G21 mm
			WorkCoordinate: 54, // G54
		},
	}
}

// WriteComment writes a comment line.
func (g *Generator) WriteComment(comment string) error {
	_, err := fmt.Fprintf(g.writer, "; %s\n", comment)
	return err
}

// WriteCommand writes a G-code command.
func (g *Generator) WriteCommand(cmd *Command) error {
	var parts []string

	if cmd.Letter != "" {
		parts = append(parts, fmt.Sprintf("%s%d", cmd.Letter, cmd.Number))
	}

	// Order parameters consistently
	paramOrder := []string{"X", "Y", "Z", "A", "B", "C", "I", "J", "K", "F", "S", "P", "Q", "R"}
	for _, param := range paramOrder {
		if val, ok := cmd.Parameters[param]; ok {
			parts = append(parts, fmt.Sprintf("%s%.3f", param, val))
		}
	}

	line := strings.Join(parts, " ")
	if cmd.Comment != "" {
		line += fmt.Sprintf(" ; %s", cmd.Comment)
	}

	_, err := fmt.Fprintln(g.writer, line)
	return err
}

// MoveTo generates a rapid move (G0).
func (g *Generator) MoveTo(x, y, z *float64) error {
	cmd := &Command{
		Letter:     "G",
		Number:     0,
		Parameters: make(map[string]float64),
	}

	if x != nil {
		cmd.Parameters["X"] = *x
	}
	if y != nil {
		cmd.Parameters["Y"] = *y
	}
	if z != nil {
		cmd.Parameters["Z"] = *z
	}

	g.modal.MotionMode = 0
	return g.WriteCommand(cmd)
}

// LinearMove generates a linear move (G1).
func (g *Generator) LinearMove(x, y, z *float64, feedRate *float64) error {
	cmd := &Command{
		Letter:     "G",
		Number:     1,
		Parameters: make(map[string]float64),
	}

	if x != nil {
		cmd.Parameters["X"] = *x
	}
	if y != nil {
		cmd.Parameters["Y"] = *y
	}
	if z != nil {
		cmd.Parameters["Z"] = *z
	}
	if feedRate != nil {
		cmd.Parameters["F"] = *feedRate
		g.modal.FeedRate = *feedRate
	} else if g.modal.FeedRate > 0 && g.modal.MotionMode != 1 {
		cmd.Parameters["F"] = g.modal.FeedRate
	}

	g.modal.MotionMode = 1
	return g.WriteCommand(cmd)
}

// Arc generates an arc move (G2/G3).
func (g *Generator) Arc(clockwise bool, x, y, z, i, j, k *float64, feedRate *float64) error {
	cmd := &Command{
		Letter:     "G",
		Number:     2,
		Parameters: make(map[string]float64),
	}

	if !clockwise {
		cmd.Number = 3
	}

	if x != nil {
		cmd.Parameters["X"] = *x
	}
	if y != nil {
		cmd.Parameters["Y"] = *y
	}
	if z != nil {
		cmd.Parameters["Z"] = *z
	}
	if i != nil {
		cmd.Parameters["I"] = *i
	}
	if j != nil {
		cmd.Parameters["J"] = *j
	}
	if k != nil {
		cmd.Parameters["K"] = *k
	}
	if feedRate != nil {
		cmd.Parameters["F"] = *feedRate
		g.modal.FeedRate = *feedRate
	}

	g.modal.MotionMode = cmd.Number
	return g.WriteCommand(cmd)
}

// Home generates a home command (G28).
func (g *Generator) Home(axes ...string) error {
	cmd := &Command{
		Letter:     "G",
		Number:     28,
		Parameters: make(map[string]float64),
	}

	for _, axis := range axes {
		cmd.Parameters[axis] = 0
	}

	return g.WriteCommand(cmd)
}

// SetUnits sets units (G20 inch / G21 mm).
func (g *Generator) SetUnits(mm bool) error {
	num := 20
	if mm {
		num = 21
	}

	g.modal.Units = num
	return g.WriteCommand(&Command{
		Letter: "G",
		Number: num,
	})
}

// SetAbsolute sets absolute positioning (G90).
func (g *Generator) SetAbsolute() error {
	g.modal.CoordinateMode = 90
	return g.WriteCommand(&Command{
		Letter: "G",
		Number: 90,
	})
}

// SetRelative sets relative positioning (G91).
func (g *Generator) SetRelative() error {
	g.modal.CoordinateMode = 91
	return g.WriteCommand(&Command{
		Letter: "G",
		Number: 91,
	})
}

// SpindleOn turns spindle on (M3/M4).
func (g *Generator) SpindleOn(clockwise bool, speed *float64) error {
	num := 3
	if !clockwise {
		num = 4
	}

	cmd := &Command{
		Letter:     "M",
		Number:     num,
		Parameters: make(map[string]float64),
	}

	if speed != nil {
		cmd.Parameters["S"] = *speed
		g.modal.SpindleSpeed = *speed
	}

	return g.WriteCommand(cmd)
}

// SpindleOff turns spindle off (M5).
func (g *Generator) SpindleOff() error {
	return g.WriteCommand(&Command{
		Letter: "M",
		Number: 5,
	})
}

// Coolant controls coolant (M7 mist, M8 flood, M9 off).
func (g *Generator) Coolant(mode string) error {
	num := 9 // off
	switch mode {
	case "mist":
		num = 7
	case "flood":
		num = 8
	}

	return g.WriteCommand(&Command{
		Letter: "M",
		Number: num,
	})
}

// ProgramEnd ends program (M30).
func (g *Generator) ProgramEnd() error {
	return g.WriteCommand(&Command{
		Letter: "M",
		Number: 30,
	})
}
