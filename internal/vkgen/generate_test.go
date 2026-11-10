package vkgen

import (
	"strings"
	"testing"
)

const miniTypes = `
<types>
  <type category="basetype">typedef <type>uint32_t</type> <name>VkFlags</name>;</type>
  <type category="basetype">typedef <type>uint64_t</type> <name>VkFlags64</name>;</type>
  <type category="basetype">typedef <type>uint64_t</type> <name>VkDeviceSize</name>;</type>
  <type category="basetype">typedef <type>void</type> <name>VkOpaque</name>;</type>
  <type category="bitmask" requires="VkXBits">typedef <type>VkFlags</type> <name>VkXFlags</name>;</type>
  <type category="bitmask">typedef <type>VkFlags64</type> <name>VkWideFlags</name>;</type>
  <type category="bitmask">typedef <type>VkOpaque</type> <name>VkBadFlags</name>;</type>
  <type category="handle"><type>VK_DEFINE_HANDLE</type>(<name>VkDisp</name>)</type>
  <type category="handle"><type>VK_DEFINE_NON_DISPATCHABLE_HANDLE</type>(<name>VkNon</name>)</type>
  <type name="VkXBits" category="enum"/>
  <type name="VkNoBlock" category="enum"/>
  <type category="struct" name="VkInner"><member><type>uint8_t</type> <name>a</name></member><member><type>uint32_t</type> <name>b</name></member></type>
  <type category="struct" name="VkAll">
    <member values="VK_STRUCTURE_TYPE_ALL"><type>VkStructureType</type> <name>sType</name></member>
    <member><type>void</type>* <name>pNext</name></member>
    <member><type>uint8_t</type> <name>small</name></member>
    <member><type>VkDisp</type> <name>disp</name></member>
    <member><type>VkNon</type> <name>non</name></member>
    <member><type>VkDeviceSize</type> <name>size</name></member>
    <member><type>VkXFlags</type> <name>flags</name></member>
    <member><type>VkWideFlags</type> <name>wide</name></member>
    <member><type>VkXBits</type> <name>bits</name></member>
    <member><type>VkNoBlock</type> <name>nb</name></member>
    <member><type>VkWideBits</type> <name>wb</name></member>
    <member><type>VkInner</type> <name>inner</name>[2][3]</member>
    <member><type>char</type> <name>name</name>[<enum>VK_N</enum>]</member>
    <member><type>double</type> <name>d</name></member>
    <member><type>size_t</type> <name>sz</name></member>
    <member><type>uint16_t</type> <name>u16</name></member>
    <member><type>VkAlias2</type> <name>viaAlias</name></member>
    <member>const <type>char</type>* const* <name>names</name></member>
  </type>
  <type category="struct" name="VkAlias2" alias="VkInner"/>
  <type category="struct" name="VkSelf"><member><type>VkSelf</type> <name>x</name></member></type>
  <type category="struct" name="VkEmpty"></type>
  <type category="struct" name="VkBits"><member><type>uint32_t</type> <name>x</name>:3</member></type>
  <type category="struct" name="VkUnknown"><member><type>VkWhat</type> <name>x</name></member></type>
  <type category="struct" name="VkBadDim"><member><type>int</type> <name>x</name>[VK_NOPE]</member></type>
  <type category="struct" name="VkTextDim"><member><type>int</type> <name>x</name>[VK_TXT]</member></type>
  <type category="struct" name="VkZeroDim"><member><type>int</type> <name>x</name>[0]</member></type>
  <type category="struct" name="VkHugeDim"><member><type>int</type> <name>x</name>[70000][70000]</member></type>
  <type category="struct" name="VkBadBase"><member><type>VkOpaque</type> <name>x</name></member></type>
  <type category="struct" name="VkBadFlag"><member><type>VkBadFlags</type> <name>x</name></member></type>
  <type category="struct" name="VkNoType"><member values="VK_STRUCTURE_TYPE_NOWHERE"><type>VkStructureType</type> <name>sType</name></member></type>
  <type category="struct" name="VkCycleA"><member><type>VkCycleB</type> <name>x</name></member></type>
  <type category="struct" name="VkCycleB"><member><type>VkCycleA</type> <name>x</name></member></type>
</types>
<enums name="API Constants" type="constants">
  <enum type="uint32_t" value="8" name="VK_N"/>
  <enum type="uint32_t" value="VK_TXTV" name="VK_TXT"/>
  <enum type="uint32_t" value="(~0U)" name="VK_U32"/>
  <enum type="uint32_t" value="(~1U)" name="VK_U32_1"/>
  <enum type="uint64_t" value="(~0ULL)" name="VK_U64"/>
  <enum type="uint32_t" value="0x10" name="VK_HEX"/>
  <enum type="uint32_t" value="1" name="VK_INNER"/>
  <enum type="uint64_t" value="7U" name="VK_SEVEN"/>
  <enum type="float" value="1000.0F" name="VK_FLOAT"/>
  <enum type="uint32_t" value="(~x)" name="VK_BADNOT"/>
  <enum type="uint32_t" value="zzz" name="VK_BADVAL"/>
</enums>
<enums name="VkStructureType" type="enum">
  <enum value="1" name="VK_STRUCTURE_TYPE_ALL"/>
  <enum value="2" name="VK_STRUCTURE_TYPE_OTHER"/>
</enums>
<enums name="VkXBits" type="bitmask">
  <enum bitpos="0" name="VK_X_ONE_BIT"/>
  <enum bitpos="5" name="VK_X_SIXTEEN_BIT"/>
</enums>
<enums name="VkWideBits" type="bitmask" bitwidth="64">
  <enum bitpos="40" name="VK_WIDE_BIG_BIT"/>
</enums>
<enums name="VkSigned" type="enum">
  <enum value="-1" name="VK_SIGNED_NEG"/>
  <enum value="3" name="VK_SIGNED_THREE"/>
</enums>
<enums name="VkClash" type="enum">
  <enum value="1" name="VK_X_ONE__BIT"/>
</enums>
<enums name="VkTypeClash" type="enum">
  <enum value="1" name="VK_INNER"/>
</enums>`

func mini(t *testing.T) *Registry { return parse(t, miniTypes) }

func TestGenerateMini(t *testing.T) {
	out, err := Generate(mini(t), Config{
		Package:   "vk",
		Structs:   []string{"VkAll", "VkAlias2"},
		Handles:   []string{"VkDisp", "VkNon"},
		Enums:     []string{"VkXBits", "VkSigned", "VkStructureType"},
		Constants: []string{"VK_U32", "VK_U32_1", "VK_U64", "VK_HEX", "VK_SEVEN"},
	})
	if err != nil {
		t.Fatal(err)
	}
	src := strings.Join(strings.Fields(string(out.Types)), " ") // gofmt aligns columns
	for _, want := range []string{
		"// Code generated by vkgen", "DO NOT EDIT",
		"type Disp uintptr", "type Non uint64", "type XBits uint32", "type WideBits uint64", "type NoBlock int32",
		"type XFlags uint32", "type WideFlags uint64", "type DeviceSize uint64", "type Flags uint32",
		"type Inner struct", "A uint8", "Inner [2][3]Inner", "Name [8]byte", "PNext unsafe.Pointer", "Names unsafe.Pointer",
		"D float64", "Sz uintptr", "U16 uint16", "ViaAlias Inner", "A uint8 B uint32",
		"const XOneBit XBits = 0x1", "const XSixteenBit XBits = 0x20", "const SignedNeg Signed = -1", "const SignedThree Signed = 3",
		"const StructureTypeAll StructureType = 1", "const StructureTypeOther StructureType = 2",
		"const U32 uint32 = 0xffffffff", "const U321 uint32 = 0xfffffffe", "const U64 uint64 = 0xffffffffffffffff", "const Hex uint32 = 0x10", "const Seven uint64 = 0x7",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("types missing %q", want)
		}
	}
	if strings.Contains(src, "StructureTypeNowhere") {
		t.Error("an unneeded structure type")
	}
	test := string(out.LayoutTest)
	// Offsets by the C rules: uint8 at 0 after the 4-byte sType? sType is an
	// int32 enum at 0, pNext pointer at 8, small uint8 at 16, Disp at 24.
	for _, want := range []string{`{"SType", unsafe.Offsetof(s0.SType), 0}`, `{"PNext", unsafe.Offsetof(s0.PNext), 8}`, `{"Small", unsafe.Offsetof(s0.Small), 16}`, `{"Disp", unsafe.Offsetof(s0.Disp), 24}`, `"Inner"`} {
		if !strings.Contains(test, want) {
			t.Errorf("layout test missing %q\n%s", want, test)
		}
	}
	// A hand-computed size: Inner is {uint8, uint32} = 8 bytes, align 4.
	if !strings.Contains(test, `checkLayout(t, "Inner", unsafe.Sizeof(s`) || !strings.Contains(test, "), 8, unsafe.Alignof(s") {
		t.Errorf("Inner layout:\n%s", test)
	}
}

func TestGenerateErrors(t *testing.T) {
	reg := mini(t)
	for name, cfg := range map[string]Config{
		"no package":       {},
		"bad package":      {Package: "not a package"},
		"constant vs type": {Package: "p", Structs: []string{"VkInner"}, Constants: []string{"VK_INNER"}},
		"unknown struct":   {Package: "p", Structs: []string{"VkNope"}},
		"unknown handle":   {Package: "p", Handles: []string{"VkNope"}},
		"unknown enum":     {Package: "p", Enums: []string{"VkNope"}},
		"unknown constant": {Package: "p", Constants: []string{"VK_NOPE"}},
		"float constant":   {Package: "p", Constants: []string{"VK_FLOAT"}},
		"bad not":          {Package: "p", Constants: []string{"VK_BADNOT"}},
		"bad value":        {Package: "p", Constants: []string{"VK_BADVAL"}},
		"struct in itself": {Package: "p", Structs: []string{"VkSelf"}},
		"cycle":            {Package: "p", Structs: []string{"VkCycleA"}},
		"empty struct":     {Package: "p", Structs: []string{"VkEmpty"}},
		"bit field":        {Package: "p", Structs: []string{"VkBits"}},
		"unknown member":   {Package: "p", Structs: []string{"VkUnknown"}},
		"unknown dim":      {Package: "p", Structs: []string{"VkBadDim"}},
		"text dim":         {Package: "p", Structs: []string{"VkTextDim"}},
		"zero dim":         {Package: "p", Structs: []string{"VkZeroDim"}},
		"huge dim":         {Package: "p", Structs: []string{"VkHugeDim"}},
		"bad base":         {Package: "p", Structs: []string{"VkBadBase"}},
		"bad flags":        {Package: "p", Structs: []string{"VkBadFlag"}},
		"missing stype":    {Package: "p", Structs: []string{"VkNoType"}},
		"const vs const":   {Package: "p", Enums: []string{"VkXBits", "VkClash"}},
		"const vs type":    {Package: "p", Structs: []string{"VkInner"}, Enums: []string{"VkTypeClash"}},
	} {
		if _, err := Generate(reg, cfg); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	// A structure type constant that collides with a type of the same Go name.
	r0 := parse(t, `<types>
	  <type category="struct" name="VkStructureTypeS"><member values="VK_STRUCTURE_TYPE_S"><type>VkStructureType</type> <name>sType</name></member></type></types>
	  <enums name="VkStructureType" type="enum"><enum value="1" name="VK_STRUCTURE_TYPE_S"/></enums>`)
	if _, err := Generate(r0, Config{Package: "p", Structs: []string{"VkStructureTypeS"}}); err == nil {
		t.Error("a structure type constant colliding with a type was accepted")
	}
	// A registry with no VkStructureType cannot satisfy an sType member.
	r := parse(t, `<types><type category="struct" name="VkS"><member values="VK_STRUCTURE_TYPE_S"><type>int</type> <name>sType</name></member></type></types>`)
	if _, err := Generate(r, Config{Package: "p", Structs: []string{"VkS"}}); err == nil {
		t.Error("an sType without VkStructureType was accepted")
	}
	// A structure with a type through an unsupported base typedef.
	r = parse(t, `<types>
	  <type category="basetype">typedef <type>VkWhat</type> <name>VkB</name>;</type>
	  <type category="struct" name="VkS"><member><type>VkB</type> <name>x</name></member></type></types>`)
	if _, err := Generate(r, Config{Package: "p", Structs: []string{"VkS"}}); err == nil {
		t.Error("a base typedef of an unknown type was accepted")
	}
	// A flags typedef over an unknown type.
	r = parse(t, `<types>
	  <type category="bitmask">typedef <type>VkWhat</type> <name>VkF</name>;</type>
	  <type category="struct" name="VkS"><member><type>VkF</type> <name>x</name></member></type></types>`)
	if _, err := Generate(r, Config{Package: "p", Structs: []string{"VkS"}}); err == nil {
		t.Error("a flags typedef of an unknown type was accepted")
	}
}

func TestNames(t *testing.T) {
	for in, want := range map[string]string{
		"VK_BUFFER_USAGE_STORAGE_BUFFER_BIT": "BufferUsageStorageBufferBit",
		"VK_SUCCESS":                         "Success",
		"VK_FORMAT_R8_UNORM":                 "FormatR8Unorm",
		"VK__DOUBLE":                         "Double",
	} {
		if got := constName(in); got != want {
			t.Errorf("%s -> %s, want %s", in, got, want)
		}
	}
	for in, want := range map[string]string{"VkBuffer": "Buffer", "Vk": "Vk", "Vkx": "Vkx", "Other": "Other"} {
		if got := goName(in); got != want {
			t.Errorf("%s -> %s", in, got)
		}
	}
}
