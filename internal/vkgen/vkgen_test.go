package vkgen

import (
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func parse(t *testing.T, body string) *Registry {
	t.Helper()
	r, err := Parse(strings.NewReader("<registry>" + body + "</registry>"))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func parseErr(t *testing.T, doc string) error {
	t.Helper()
	_, err := Parse(strings.NewReader(doc))
	if err == nil {
		t.Fatalf("Parse accepted %q", doc)
	}
	return err
}

const realRegistry = "../../registry/vk.xml.gz"

// TestRealRegistry reads the pinned vk.xml and checks facts that are stated by
// the Vulkan specification, not by this generator.
func TestRealRegistry(t *testing.T) {
	f, err := OpenRegistry(realRegistry)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	reg, err := Parse(f)
	if err != nil {
		t.Fatal(err)
	}
	if reg.Version != "363" {
		t.Errorf("VK_HEADER_VERSION = %q", reg.Version)
	}
	vals := map[string]int64{}
	for _, v := range reg.Enums["VkResult"].Values {
		vals[v.Name] = v.Value
	}
	// Values from the Vulkan specification: core, extension offset, negative
	// extension offset, and an alias.
	for name, want := range map[string]int64{
		"VK_SUCCESS": 0, "VK_NOT_READY": 1, "VK_TIMEOUT": 2, "VK_INCOMPLETE": 5,
		"VK_ERROR_OUT_OF_HOST_MEMORY": -1, "VK_ERROR_DEVICE_LOST": -4,
		"VK_ERROR_OUT_OF_DATE_KHR":    -1000001004,
		"VK_ERROR_OUT_OF_POOL_MEMORY": -1000069000,
	} {
		if got, ok := vals[name]; !ok || got != want {
			t.Errorf("%s = %d (present %v), want %d", name, got, ok, want)
		}
	}
	st := map[string]int64{}
	for _, v := range reg.Enums["VkStructureType"].Values {
		st[v.Name] = v.Value
	}
	for name, want := range map[string]int64{
		"VK_STRUCTURE_TYPE_APPLICATION_INFO":                         0,
		"VK_STRUCTURE_TYPE_BUFFER_CREATE_INFO":                       12,
		"VK_STRUCTURE_TYPE_PHYSICAL_DEVICE_PROPERTIES_2":             1000059001,
		"VK_STRUCTURE_TYPE_PHYSICAL_DEVICE_MAINTENANCE_3_PROPERTIES": 1000168000,
		"VK_STRUCTURE_TYPE_PHYSICAL_DEVICE_DRIVER_PROPERTIES":        1000196000,
	} {
		if got, ok := st[name]; !ok || got != want {
			t.Errorf("%s = %d (present %v), want %d", name, got, ok, want)
		}
	}
	bu := map[string]int64{}
	for _, v := range reg.Enums["VkBufferUsageFlagBits"].Values {
		bu[v.Name] = v.Value
	}
	if bu["VK_BUFFER_USAGE_STORAGE_BUFFER_BIT"] != 0x20 || bu["VK_BUFFER_USAGE_UNIFORM_BUFFER_BIT"] != 0x10 {
		t.Errorf("buffer usage bits: %v", bu)
	}
	if c := reg.Constants["VK_WHOLE_SIZE"]; c.Value != "(~0ULL)" || c.Type != "uint64_t" {
		t.Errorf("VK_WHOLE_SIZE = %+v", c)
	}
	s := reg.Structs["VkBufferCreateInfo"]
	if s == nil || len(s.Members) != 8 || s.Members[3].Name != "size" || s.Members[3].Type != "VkDeviceSize" || !s.Members[7].Pointer {
		t.Errorf("VkBufferCreateInfo = %+v", s)
	}
	// The SC variant of a member does not duplicate the Vulkan one.
	n := 0
	for _, m := range reg.Structs["VkPipelineShaderStageCreateInfo"].Members {
		if m.Name == "pName" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("pName appears %d times", n)
	}
}

// TestGeneratedFilesAreCurrent is the drift test: the committed generated files
// are exactly what the pinned registry and DefaultConfig produce.
func TestGeneratedFilesAreCurrent(t *testing.T) {
	f, err := OpenRegistry(realRegistry)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	reg, err := Parse(f)
	if err != nil {
		t.Fatal(err)
	}
	out, err := Generate(reg, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string][]byte{"types_gen.go": out.Types, "layout_gen_test.go": out.LayoutTest} {
		have, err := os.ReadFile(filepath.Join("..", "vk", name))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(have, want) {
			t.Errorf("internal/vk/%s is out of date: run go generate ./internal/vk", name)
		}
	}
	again, err := Generate(reg, DefaultConfig())
	if err != nil || !bytes.Equal(again.Types, out.Types) || !bytes.Equal(again.LayoutTest, out.LayoutTest) {
		t.Fatal("two generations differ")
	}
}

func TestParseSynthetic(t *testing.T) {
	r := parse(t, `
<types>
  <type category="define">#define <name>VK_HEADER_VERSION</name> 42</type>
  <type category="define">// a define without a version <name>OTHER</name> 1</type>
  <type category="define"><name>VK_HEADER_VERSION</name></type>
  <type category="basetype">typedef <type>uint32_t</type> <name>VkFlags</name>;</type>
  <type category="basetype">struct <name>ANativeWindow</name>;</type>
  <type category="bitmask" requires="VkXBits">typedef <type>VkFlags</type> <name>VkXFlags</name>;</type>
  <type category="bitmask" name="VkAlias" alias="VkXFlags"/>
  <type category="bitmask" name="VkAttrName"><type>VkFlags</type></type>
  <type category="handle" objtypeenum="X"><type>VK_DEFINE_HANDLE</type>(<name>VkH</name>)</type>
  <type category="handle"><type>VK_DEFINE_NON_DISPATCHABLE_HANDLE</type>(<name>VkN</name>)</type>
  <type name="VkEnumT" category="enum"/>
  <type category="enum"/>
  <type category="include">#include "x.h"</type>
  <type category="struct" name="VkS">
    <member values="VK_STRUCTURE_TYPE_S"><type>VkStructureType</type> <name>sType</name></member>
    <member optional="true">const <type>void</type>*            <name>pNext</name><comment>x</comment></member>
    <member><type>char</type> <name>text</name>[<enum>VK_N</enum>]</member>
    <member><type>float</type> <name>m</name>[2][<enum>3</enum>]</member>
    <member><type>uint32_t</type> <name>bits</name>:8</member>
    <member api="vulkansc"><type>uint32_t</type> <name>scOnly</name></member>
    <comment>ignored</comment>
  </type>
  <type category="struct" name="VkS2" alias="VkS"/>
  <type category="struct" api="vulkansc" name="VkSC"><member><type>int</type> <name>x</name></member></type>
  <type category="union" name="VkU"><member><type>int</type> <name>x</name></member></type>
</types>
<enums name="API Constants" type="constants">
  <enum type="uint32_t" value="256" name="VK_N"/>
  <enum type="uint32_t" value="(~0U)" name="VK_ALL"/>
  <enum name="VK_N2" alias="VK_N"/>
  <enum value="1"/>
</enums>
<enums name="VkE" type="enum">
  <enum value="0" name="VK_E_A"/>
  <enum value="-3" name="VK_E_B"/>
  <enum value="0x10" name="VK_E_C"/>
  <enum value="VK_X + 1" name="VK_E_MACRO"/>
  <enum value="9" name="VK_E_SC" api="vulkansc"/>
  <enum name="VK_E_ALIAS" alias="VK_E_A"/>
  <enum name="VK_E_ALIAS2" alias="VK_E_ALIAS"/>
  <enum name="VK_E_NOWHERE" alias="VK_E_MISSING"/>
  <enum name="VK_E_NOVALUE"/>
  <enum value="0"/>
</enums>
<enums name="VkBits" type="bitmask" bitwidth="64">
  <enum bitpos="3" name="VK_BITS_C"/>
</enums>
<enums name="VkNotEnum" type="constants"><enum value="1" name="VK_NOT"/></enums>
<enums type="enum"><enum value="1" name="VK_ANON"/></enums>
<feature api="vulkan" name="VK_VERSION_1_1" number="1.1">
  <require>
    <enum extends="VkE" extnumber="60" offset="1" name="VK_E_FEAT"/>
    <enum extends="VkE" extnumber="60" offset="2" dir="-" name="VK_E_FEATNEG"/>
    <enum extends="VkE" name="VK_E_FEATALIAS" alias="VK_E_FEAT"/>
    <enum name="VK_PLAIN" value="1"/>
    <feature name="x" struct="y"/>
  </require>
</feature>
<extensions>
  <extension name="VK_X" number="3">
    <require>
      <enum extends="VkE" offset="4" name="VK_E_EXT"/>
      <enum extends="VkE" offset="4" name="VK_E_EXT"/>
      <enum extends="VkE" bitpos="2" name="VK_E_EXTBIT"/>
      <enum extends="VkE" value="77" name="VK_E_EXTVAL"/>
      <enum extends="VkUndeclared" value="77" name="VK_E_ELSEWHERE"/>
      <enum extends="VkE" name="VK_E_EMPTYEXT"/>
      <enum extends="VkE" value="x" name="VK_E_MACROEXT"/>
    </require>
    <require api="vulkansc">
      <enum extends="VkE" offset="9" name="VK_E_SCEXT"/>
    </require>
    <require>
      <enum extends="VkE" offset="9" api="vulkansc" name="VK_E_SCEXT2"/>
      <enum extends="VkE" offset="5" extnumber="11" name="VK_E_OVERRIDE"/>
    </require>
  </extension>
</extensions>`)
	if r.Version != "42" {
		t.Errorf("version %q", r.Version)
	}
	if r.Flags["VkAttrName"] != "VkFlags" {
		t.Errorf("a name given as an attribute: %v", r.Flags)
	}
	if r.Base["VkFlags"] != "uint32_t" || r.Flags["VkXFlags"] != "VkFlags" || r.Aliases["VkAlias"] != "VkXFlags" ||
		!r.Handles["VkH"] || r.Handles["VkN"] || !r.EnumTypes["VkEnumT"] || r.Aliases["VkS2"] != "VkS" || r.Aliases["VK_N2"] != "VK_N" {
		t.Errorf("types: %+v", r)
	}
	if _, ok := r.Base["ANativeWindow"]; ok {
		t.Error("an opaque typedef was recorded")
	}
	if c := r.Constants["VK_ALL"]; c.Value != "(~0U)" || c.Type != "uint32_t" {
		t.Errorf("%+v", c)
	}
	s := r.Structs["VkS"]
	if s == nil || len(s.Members) != 5 {
		t.Fatalf("%+v", s)
	}
	want := []Member{
		{Name: "sType", Type: "VkStructureType", Values: "VK_STRUCTURE_TYPE_S"},
		{Name: "pNext", Type: "void", Pointer: true},
		{Name: "text", Type: "char", Dims: []string{"VK_N"}},
		{Name: "m", Type: "float", Dims: []string{"2", "3"}},
		{Name: "bits", Type: "uint32_t", Bits: "8"},
	}
	for i, m := range want {
		g := s.Members[i]
		if g.Name != m.Name || g.Type != m.Type || g.Pointer != m.Pointer || g.Bits != m.Bits || g.Values != m.Values || strings.Join(g.Dims, ",") != strings.Join(m.Dims, ",") {
			t.Errorf("member %d = %+v, want %+v", i, g, m)
		}
	}
	if r.Structs["VkSC"] != nil || r.Structs["VkU"] != nil {
		t.Error("a Vulkan SC struct or a union was recorded")
	}
	vals := map[string]int64{}
	for _, v := range r.Enums["VkE"].Values {
		vals[v.Name] = v.Value
	}
	wantE := map[string]int64{
		"VK_E_A": 0, "VK_E_B": -3, "VK_E_C": 16, "VK_E_ALIAS": 0, "VK_E_ALIAS2": 0,
		"VK_E_FEAT": 1_000_059_001, "VK_E_FEATNEG": -1_000_059_002, "VK_E_FEATALIAS": 1_000_059_001,
		"VK_E_EXT": 1_000_002_004, "VK_E_EXTBIT": 4, "VK_E_EXTVAL": 77, "VK_E_OVERRIDE": 1_000_010_005,
	}
	for k, v := range wantE {
		if got, ok := vals[k]; !ok || got != v {
			t.Errorf("%s = %d (present %v), want %d", k, got, ok, v)
		}
	}
	for _, k := range []string{"VK_E_MACRO", "VK_E_SC", "VK_E_NOWHERE", "VK_E_NOVALUE", "VK_E_SCEXT", "VK_E_SCEXT2", "VK_E_ELSEWHERE", "VK_E_EMPTYEXT", "VK_E_MACROEXT"} {
		if _, ok := vals[k]; ok {
			t.Errorf("%s must not be an enumerator", k)
		}
	}
	if len(vals) != len(wantE) {
		t.Errorf("enumerators %v", vals)
	}
	b := r.Enums["VkBits"]
	if !b.Bitmask || !b.Wide || len(b.Values) != 1 || b.Values[0].Value != 8 {
		t.Errorf("%+v", b)
	}
	if r.Enums["VkNotEnum"] != nil || r.Enums[""] != nil {
		t.Error("a constants block or an anonymous block is an enumeration")
	}
}

func TestParseErrors(t *testing.T) {
	for name, doc := range map[string]string{
		"empty":                    "",
		"no registry":              "<other/>",
		"not xml":                  "<registry><types></registry>",
		"truncated":                "<registry><types><type category=\"struct\" name=\"VkS\"><member><type>int</type>",
		"truncated struct":         "<registry><types><type category=\"struct\" name=\"VkS\">",
		"truncated basetype":       "<registry><types><type category=\"basetype\">typedef <type>",
		"truncated enums":          "<registry><enums name=\"VkE\" type=\"enum\"><enum value=\"1\" name=\"A\">",
		"truncated constants":      "<registry><enums name=\"API Constants\"><enum name=\"A\"",
		"truncated enums skip":     "<registry><enums name=\"VkE\" type=\"enum\"><enum",
		"truncated struct skip":    "<registry><types><type category=\"struct\" name=\"VkS\"><comment>",
		"truncated enum skip":      "<registry><enums name=\"VkE\" type=\"enum\"><comment>",
		"truncated type skip":      "<registry><types><type category=\"enum\" name=\"VkS\"><x>",
		"truncated alias skip":     "<registry><types><type category=\"enum\" name=\"VkS\" alias=\"VkT\"><x>",
		"truncated define":         "<registry><types><type category=\"define\"><name>",
		"truncated constants skip": "<registry><enums name=\"API Constants\"><comment>",
		"member no name":           "<registry><types><type category=\"struct\" name=\"VkS\"><member><type>int</type></member></type></types></registry>",
		"member no type":           "<registry><types><type category=\"struct\" name=\"VkS\"><member><name>x</name></member></type></types></registry>",
		"unterminated dim":         "<registry><types><type category=\"struct\" name=\"VkS\"><member><type>int</type> <name>x</name>[4</member></type></types></registry>",
		"bad bitpos":               "<registry><enums name=\"VkE\" type=\"bitmask\"><enum bitpos=\"99\" name=\"A\"/></enums></registry>",
		"bad bitpos text":          "<registry><enums name=\"VkE\" type=\"bitmask\"><enum bitpos=\"x\" name=\"A\"/></enums></registry>",
		"bad offset":               "<registry><extension number=\"1\"><require><enum extends=\"VkE\" offset=\"5000\" name=\"A\"/></require></extension></registry>",
		"offset without ext":       "<registry><feature api=\"vulkan\"><require><enum extends=\"VkE\" offset=\"1\" name=\"A\"/></require></feature></registry>",
		"offset text":              "<registry><extension number=\"1\"><require><enum extends=\"VkE\" offset=\"x\" name=\"A\"/></require></extension></registry>",
	} {
		if _, err := Parse(strings.NewReader(doc)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	// A reader that fails after some bytes.
	if _, err := Parse(io.MultiReader(strings.NewReader("<registry>"), errReader{})); err == nil {
		t.Error("a failing reader was accepted")
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("disk on fire") }

func TestParseSizeLimit(t *testing.T) {
	big := io.MultiReader(strings.NewReader("<registry><!--"), &filler{left: MaxRegistryBytes + 10}, strings.NewReader("--></registry>"))
	if err := parseErrFrom(t, big); !strings.Contains(err.Error(), "larger") {
		t.Fatal(err)
	}
}

func parseErrFrom(t *testing.T, r io.Reader) error {
	t.Helper()
	_, err := Parse(r)
	if err == nil {
		t.Fatal("accepted")
	}
	return err
}

// filler yields n bytes of 'a': the inside of a very long XML comment.
type filler struct{ left int }

func (z *filler) Read(p []byte) (int, error) {
	if z.left == 0 {
		return 0, io.EOF
	}
	n := min(len(p), z.left)
	for i := range p[:n] {
		p[i] = 'a'
	}
	z.left -= n
	return n, nil
}

func TestOpenRegistry(t *testing.T) {
	dir := t.TempDir()
	plain := filepath.Join(dir, "r.xml")
	if err := os.WriteFile(plain, []byte("<registry/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	var z bytes.Buffer
	zw := gzip.NewWriter(&z)
	zw.Write([]byte("<registry/>"))
	zw.Close()
	gz := filepath.Join(dir, "r.xml.gz")
	if err := os.WriteFile(gz, z.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	notgz := filepath.Join(dir, "bad.xml.gz")
	if err := os.WriteFile(notgz, []byte("<registry/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{plain, gz} {
		rc, err := OpenRegistry(p)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Parse(rc); err != nil {
			t.Errorf("%s: %v", p, err)
		}
		if err := rc.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := OpenRegistry(notgz); err == nil {
		t.Error("a file that is not gzip was accepted as .gz")
	}
	if _, err := OpenRegistry(filepath.Join(dir, "missing")); err == nil {
		t.Error("a missing file was opened")
	}
	// A gzip bomb: more than MaxRegistryBytes once inflated, a few KB on disk.
	var bomb bytes.Buffer
	bw := gzip.NewWriter(&bomb)
	bw.Write([]byte("<registry><!--"))
	chunk := bytes.Repeat([]byte("a"), 1<<20)
	for range MaxRegistryBytes>>20 + 2 {
		bw.Write(chunk)
	}
	bw.Write([]byte("--></registry>"))
	bw.Close()
	if bomb.Len() > 1<<20 {
		t.Fatalf("the bomb is %d bytes: not a bomb", bomb.Len())
	}
	path := filepath.Join(dir, "bomb.xml.gz")
	if err := os.WriteFile(path, bomb.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	rc, err := OpenRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	if _, err := Parse(rc); err == nil || !strings.Contains(err.Error(), "larger") {
		t.Fatalf("gzip bomb: %v", err)
	}
}

func TestParseInt(t *testing.T) {
	for in, want := range map[string]int64{"12": 12, " -4 ": -4, "0x20": 32} {
		if got, err := ParseInt(in); err != nil || got != want {
			t.Errorf("%q: %d %v", in, got, err)
		}
	}
	if _, err := ParseInt("(~0U)"); err == nil {
		t.Error("a macro was a number")
	}
}
