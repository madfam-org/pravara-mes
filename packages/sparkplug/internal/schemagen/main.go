// Command schemagen regenerates sparkplugpb/sparkplug_b.pb.go and the
// human-readable sparkplugpb/sparkplug_b.proto from the Go-declared schema in
// schema.go. It needs no protoc: the descriptor is handed directly to the
// protoc-gen-go code generator library.
//
// Usage (from packages/sparkplug): go generate ./sparkplugpb
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	gengo "google.golang.org/protobuf/cmd/protoc-gen-go/internal_gengo"
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/pluginpb"

	"github.com/madfam-org/pravara-mes/packages/sparkplug/internal/schema"
)

func main() {
	out := flag.String("out", ".", "output directory for the generated files")
	flag.Parse()
	if err := run(*out); err != nil {
		fmt.Fprintln(os.Stderr, "schemagen:", err)
		os.Exit(1)
	}
}

func run(outDir string) error {
	fd := schema.FileDescriptor()
	req := &pluginpb.CodeGeneratorRequest{
		FileToGenerate: []string{schema.ProtoFile},
		Parameter:      proto.String("paths=source_relative"),
		ProtoFile:      []*descriptorpb.FileDescriptorProto{fd},
	}
	gen, err := protogen.Options{}.New(req)
	if err != nil {
		return fmt.Errorf("plugin setup: %w", err)
	}
	for _, f := range gen.Files {
		if f.Generate {
			gengo.GenerateFile(gen, f)
		}
	}
	gen.SupportedFeatures = gengo.SupportedFeatures
	resp := gen.Response()
	if resp.Error != nil {
		return fmt.Errorf("generate: %s", resp.GetError())
	}
	for _, f := range resp.File {
		if err := os.WriteFile(filepath.Join(outDir, f.GetName()), []byte(f.GetContent()), 0o644); err != nil {
			return err
		}
	}
	return os.WriteFile(filepath.Join(outDir, schema.ProtoFile), []byte(schema.RenderProto(fd)), 0o644)
}
