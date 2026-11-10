// Command vkgen generates the Go mirrors of the Vulkan structures that
// go-gfx/vulkan uses from the Khronos registry (vk.xml).
//
//	vkgen --registry registry/vk.xml.gz --out internal/vk
//	vkgen --registry registry/vk.xml.gz --out internal/vk --check
//
// It writes types_gen.go and layout_gen_test.go; with --check it writes nothing
// and fails when the files on disk differ from what the registry yields.
package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/go-gfx/vulkan/internal/vkgen"
	"github.com/spf13/cobra"
)

var osExit = os.Exit

func main() {
	if err := RootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "vkgen:", err)
		osExit(1)
	}
}

// RootCmd builds the vkgen command.
func RootCmd() *cobra.Command {
	var registry, out, pkg string
	var check bool
	cmd := &cobra.Command{
		Use:           "vkgen",
		Short:         "Generate Go struct mirrors from the Khronos Vulkan registry",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			f, err := vkgen.OpenRegistry(registry)
			if err != nil {
				return err
			}
			defer f.Close()
			reg, err := vkgen.Parse(f)
			if err != nil {
				return err
			}
			cfg := vkgen.DefaultConfig()
			cfg.Package = pkg
			o, err := vkgen.Generate(reg, cfg)
			if err != nil {
				return err
			}
			files := []struct {
				name string
				data []byte
			}{{"types_gen.go", o.Types}, {"layout_gen_test.go", o.LayoutTest}}
			for _, file := range files {
				path := filepath.Join(out, file.name)
				if check {
					have, err := os.ReadFile(path)
					if err != nil {
						return err
					}
					if !bytes.Equal(have, file.data) {
						return fmt.Errorf("%s is out of date: run vkgen", path)
					}
					continue
				}
				if err := os.WriteFile(path, file.data, 0o644); err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), "wrote", path)
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&registry, "registry", "r", "registry/vk.xml.gz", "path to vk.xml, or to a gzipped one (.gz)")
	cmd.Flags().StringVarP(&out, "out", "o", ".", "output directory")
	cmd.Flags().StringVar(&pkg, "package", "vk", "Go package name of the output")
	cmd.Flags().BoolVar(&check, "check", false, "fail if the files on disk differ, write nothing")
	return cmd
}
