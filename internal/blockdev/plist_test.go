package blockdev

import (
	"reflect"
	"testing"
)

const plistBasicsFixture = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>aString</key>
	<string>hello</string>
	<key>anInteger</key>
	<integer>17903616</integer>
	<key>aTrue</key>
	<true/>
	<key>aFalse</key>
	<false/>
	<key>aData</key>
	<data>
	aGVsbG8=
	</data>
	<key>anArray</key>
	<array>
		<string>a</string>
		<integer>2</integer>
	</array>
	<key>aReal</key>
	<real>1.5</real>
	<key>afterReal</key>
	<string>survived</string>
	<key>aNestedDict</key>
	<dict>
		<key>inner</key>
		<true/>
	</dict>
</dict>
</plist>
`

func TestDecodePlistBasics(t *testing.T) {
	v, err := decodePlist([]byte(plistBasicsFixture))
	if err != nil {
		t.Fatalf("decodePlist: %v", err)
	}
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("root = %T, want map[string]any", v)
	}

	if got := m["aString"]; got != "hello" {
		t.Errorf("aString = %#v, want \"hello\"", got)
	}
	if got := m["anInteger"]; got != int64(17903616) {
		t.Errorf("anInteger = %#v, want int64(17903616)", got)
	}
	if got := m["aTrue"]; got != true {
		t.Errorf("aTrue = %#v, want true", got)
	}
	if got := m["aFalse"]; got != false {
		t.Errorf("aFalse = %#v, want false", got)
	}
	if got := m["aData"]; got != "aGVsbG8=" {
		t.Errorf("aData = %#v, want \"aGVsbG8=\" (whitespace stripped)", got)
	}
	if got, want := m["anArray"], []any{"a", int64(2)}; !reflect.DeepEqual(got, want) {
		t.Errorf("anArray = %#v, want %#v", got, want)
	}
	inner, ok := m["aNestedDict"].(map[string]any)
	if !ok || inner["inner"] != true {
		t.Errorf("aNestedDict = %#v, want map with inner=true", m["aNestedDict"])
	}
}

func TestDecodePlistSkipsUnknownElementTypes(t *testing.T) {
	v, err := decodePlist([]byte(plistBasicsFixture))
	if err != nil {
		t.Fatalf("decodePlist: %v", err)
	}
	m := v.(map[string]any)

	if _, present := m["aReal"]; present {
		t.Errorf("aReal should have been omitted, got %#v", m["aReal"])
	}
	if got := m["afterReal"]; got != "survived" {
		t.Errorf("afterReal = %#v, want \"survived\" (decoding should continue past a skipped element)", got)
	}
}

func TestDecodePlistTopLevelArray(t *testing.T) {
	const fixture = `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0">
<array>
	<dict>
		<key>k</key>
		<string>v</string>
	</dict>
</array>
</plist>
`
	v, err := decodePlist([]byte(fixture))
	if err != nil {
		t.Fatalf("decodePlist: %v", err)
	}
	arr, ok := v.([]any)
	if !ok || len(arr) != 1 {
		t.Fatalf("root = %#v, want a one-element []any", v)
	}
	d, ok := arr[0].(map[string]any)
	if !ok || d["k"] != "v" {
		t.Errorf("arr[0] = %#v, want map with k=v", arr[0])
	}
}

func TestDecodePlistInvalidXMLErrors(t *testing.T) {
	if _, err := decodePlist([]byte("not xml at all")); err == nil {
		t.Fatal("expected an error for malformed input")
	}
}
