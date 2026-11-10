package vkgen

import (
	"bytes"
	"io"
	"testing"
)

// FuzzParse feeds arbitrary bytes to the registry reader and, when they parse,
// to the generator under the default configuration and under one that asks for
// every structure the document declares. Neither may panic or loop, and a
// parse that succeeds must generate deterministically. The corpus under
// testdata/fuzz/FuzzParse is committed.
func FuzzParse(f *testing.F) {
	f.Add([]byte("<registry/>"))
	f.Add([]byte(`<registry><types><type category="struct" name="VkS"><member values="VK_STRUCTURE_TYPE_S"><type>VkStructureType</type> <name>sType</name></member><member><type>char</type> <name>n</name>[<enum>VK_N</enum>]</member></type></types><enums name="VkStructureType" type="enum"><enum value="1" name="VK_STRUCTURE_TYPE_S"/></enums><enums name="API Constants"><enum type="uint32_t" value="4" name="VK_N"/></enums></registry>`))
	f.Add([]byte(`<registry><extension number="3"><require><enum extends="VkE" offset="4" dir="-" name="A"/><enum extends="VkE" alias="A" name="B"/></require></extension><enums name="VkE" type="enum"/></registry>`))
	if rc, err := OpenRegistry(realRegistry); err == nil {
		b, _ := io.ReadAll(rc)
		rc.Close()
		// A slice of the real registry: its first types, cut at a tag boundary.
		if i := bytes.Index(b, []byte("<enums name=\"VkResult\"")); i > 0 && i < 1<<20 {
			f.Add(append(append([]byte{}, b[:i]...), []byte("</registry>")...))
		}
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		reg, err := Parse(bytes.NewReader(data))
		if err != nil {
			return
		}
		cfg := Config{Package: "p"}
		for name := range reg.Structs {
			cfg.Structs = append(cfg.Structs, name)
		}
		for name := range reg.Handles {
			cfg.Handles = append(cfg.Handles, name)
		}
		for name := range reg.Enums {
			cfg.Enums = append(cfg.Enums, name)
		}
		for name := range reg.Constants {
			cfg.Constants = append(cfg.Constants, name)
		}
		// Whole-document generation may legitimately fail; it must not panic.
		_, _ = Generate(reg, cfg)
		// One structure at a time: when it generates, it generates the same twice.
		for name := range reg.Structs {
			one := Config{Package: "p", Structs: []string{name}}
			a, errA := Generate(reg, one)
			b, errB := Generate(reg, one)
			if (errA == nil) != (errB == nil) || (errA == nil && (!bytes.Equal(a.Types, b.Types) || !bytes.Equal(a.LayoutTest, b.LayoutTest))) {
				t.Fatalf("generation of %s is not deterministic", name)
			}
		}
		_, _ = Generate(reg, DefaultConfig())
	})
}
